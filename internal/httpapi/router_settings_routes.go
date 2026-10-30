package httpapi

import (
	"context"
	"errors"
	"io"
	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"itmap/internal/integrations/aihttp"
	"net/http"
	"strings"
	"time"
)

type RouterSettingsService interface {
	Public(context.Context, bool) (map[string]any, error)
	Generation(context.Context) (string, error)
	Save(context.Context, string, []byte) (map[string]any, error)
	Check(context.Context) (map[string]any, error)
	Refresh(context.Context) (map[string]any, error)
	Logs(context.Context) (map[string]any, error)
}

func routerSettingsError(w http.ResponseWriter, err error) {
	code, status, message := "storage", 503, "Не удалось выполнить операцию OpenRouter."
	var input *aiproviders.InputError
	var provider *aihttp.Error
	var service *aiproviders.ServiceError
	switch {
	case errors.As(err, &input):
		code, status, message = "input", input.Status, input.Error()
	case errors.As(err, &service):
		code, status, message = service.Code, service.Status, service.Error()
	case errors.As(err, &provider):
		code, status, message = provider.Code, provider.Status, provider.Error()
	case errors.Is(err, aiproviders.ErrDecrypt):
		code, status, message = "decrypt", 400, "Не удалось расшифровать ключ. Сохраните API-ключ заново."
	case errors.Is(err, aiproviders.ErrMissing):
		code, status, message = "missing_key", 400, "Укажите и сохраните API-ключ OpenRouter."
	case errors.Is(err, aiproviders.ErrChanged):
		code, status, message = "changed", 409, "Настройки изменились во время запроса. Повторите действие."
	case errors.Is(err, aiproviders.ErrGeneration):
		code, status, message = "restored", 409, "Данные восстановлены во время операции. Повторите действие."
	}
	jsonReply(w, status, map[string]any{"error": message, "code": code})
}
func RouterSettingsHandler(manager *auth.Manager, service RouterSettingsService, tests ...AIConnectionTests) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		action := strings.TrimPrefix(r.URL.EscapedPath(), "/api/openrouter")
		methods := map[string]string{"": "GET", "/key": "PUT", "/check": "POST", "/refresh": "POST", "/test": "POST", "/logs": "GET", "/jev": "GET"}
		method, known := methods[action]
		if !strings.HasPrefix(r.URL.EscapedPath(), "/api/openrouter") || !known {
			jsonReply(w, 404, map[string]string{"error": "Метод API не найден."})
			return
		}
		if r.Method != method {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if r.Method != "GET" && !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if action == "/test" {
			runAIConnectionTest(w, r, aihttp.OpenRouter, tests)
			return
		}
		if action == "/jev" {
			jsonReply(w, 503, map[string]string{"error": "Эта операция OpenRouter ещё не подключена.", "code": "backend_not_migrated"})
			return
		}
		if service == nil {
			routerSettingsError(w, aiproviders.ErrStorage)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		var result map[string]any
		var err error
		switch action {
		case "/key":
			g, e := service.Generation(ctx)
			if e != nil {
				routerSettingsError(w, e)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2048)
			body, e := io.ReadAll(r.Body)
			if e != nil {
				var limit *http.MaxBytesError
				if errors.As(e, &limit) {
					routerSettingsError(w, &aiproviders.InputError{Status: 413, Message: "Слишком длинный ключ."})
				} else {
					routerSettingsError(w, &aiproviders.InputError{Status: 400, Message: "Некорректный JSON."})
				}
				return
			}
			result, err = service.Save(ctx, g, body)
		case "/check":
			result, err = service.Check(ctx)
		case "/refresh":
			result, err = service.Refresh(ctx)
		case "/logs":
			result, err = service.Logs(ctx)
		default:
			result, err = service.Public(ctx, false)
		}
		if err != nil {
			routerSettingsError(w, err)
			return
		}
		jsonReply(w, 200, result)
	})
}
