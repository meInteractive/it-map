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

type OpenAISettingsService interface {
	Public(context.Context, bool) (map[string]any, error)
	Generation(context.Context) (string, error)
	Save(context.Context, string, []byte) (map[string]any, error)
}

func aiSettingsError(w http.ResponseWriter, err error) {
	code, status, message := "storage", 503, "Не удалось выполнить операцию OpenAI."
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
		code, status, message = "decrypt", 409, "Не удалось расшифровать ключ OpenAI. Сохраните его заново."
	case errors.Is(err, aiproviders.ErrMissing):
		code, status, message = "missing_key", 409, "Сохраните API-ключ OpenAI во вкладке «Нейросети» → «ChatGPT»."
	case errors.Is(err, aiproviders.ErrChanged):
		code, status, message = "changed", 409, "Настройки изменились во время запроса. Повторите действие."
	case errors.Is(err, aiproviders.ErrGeneration):
		code, status, message = "restored", 409, "Данные восстановлены во время операции. Повторите действие."
	}
	jsonReply(w, status, map[string]any{"error": message, "code": code})
}
func OpenAISettingsHandler(manager *auth.Manager, service OpenAISettingsService, tests ...AIConnectionTests) http.HandlerFunc {
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
		path := r.URL.EscapedPath()
		action := strings.TrimPrefix(path, "/api/openai")
		if path != "/api/openai" && path != "/api/openai/key" && path != "/api/openai/refresh" && path != "/api/openai/test" {
			jsonReply(w, 404, map[string]string{"error": "Метод API не найден."})
			return
		}
		method := "GET"
		if action == "/key" {
			method = "PUT"
		} else if action != "" {
			method = "POST"
		}
		if r.Method != method {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if action != "" && !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if action == "/test" {
			runAIConnectionTest(w, r, aihttp.OpenAI, tests)
			return
		}
		if service == nil {
			aiSettingsError(w, aiproviders.ErrStorage)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		var result map[string]any
		var err error
		if action == "/key" {
			// Фиксируем generation до чтения потенциально медленного body.
			generation, e := service.Generation(ctx)
			if e != nil {
				aiSettingsError(w, e)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2048)
			body, e := io.ReadAll(r.Body)
			if e != nil {
				var limit *http.MaxBytesError
				if errors.As(e, &limit) {
					aiSettingsError(w, &aiproviders.InputError{Status: 413, Message: "Слишком длинный ключ."})
				} else {
					aiSettingsError(w, &aiproviders.InputError{Status: 400, Message: "Некорректный JSON."})
				}
				return
			}
			result, err = service.Save(ctx, generation, body)
		} else {
			result, err = service.Public(ctx, action == "/refresh")
		}
		if err != nil {
			aiSettingsError(w, err)
			return
		}
		jsonReply(w, 200, result)
	})
}
