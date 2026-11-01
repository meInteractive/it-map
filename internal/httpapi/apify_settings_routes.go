package httpapi

import (
	"context"
	"errors"
	"io"
	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ApifySettingsService interface {
	Read(context.Context) (apifysettings.PublicView, error)
	Generation(context.Context) (string, error)
	SaveKey(context.Context, string, apifysettings.KeyInput) (apifysettings.PublicView, error)
	SaveSettings(context.Context, string, apifysettings.SettingsInput) (apifysettings.PublicView, error)
	ReadHHSource(context.Context, int64) (apifysettings.HHSource, error)
	SaveHHSource(context.Context, string, int64, apifysettings.HHSourceInput) (apifysettings.HHSource, error)
}

func apifySettingsError(w http.ResponseWriter, err error) {
	code, status, message := "apify_storage", http.StatusServiceUnavailable, "Не удалось выполнить операцию Apify."
	switch {
	case errors.Is(err, apifysettings.ErrInput):
		code, status, message = "apify_data", http.StatusBadRequest, "Некорректные настройки Apify."
	case errors.Is(err, apifysettings.ErrDisabled):
		code, status, message = "apify_disabled", http.StatusBadRequest, "Apify выключен или API-токен не настроен."
	case errors.Is(err, apifysettings.ErrChanged):
		code, status, message = "apify_changed", http.StatusConflict, "Настройки Apify изменились. Повторите действие."
	case errors.Is(err, apifysettings.ErrGeneration):
		code, status, message = "restored", http.StatusConflict, "Данные восстановлены во время операции. Повторите действие."
	case errors.Is(err, apifysettings.ErrSourceMissing):
		code, status, message = "source_missing", http.StatusNotFound, "Источник HH организации не найден."
	}
	jsonReply(w, status, map[string]any{"error": message, "code": code})
}

func ApifySettingsHandler(manager *auth.Manager, service ApifySettingsService) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		path := r.URL.EscapedPath()
		legacySource := false
		legacyCompany := ""
		if strings.HasPrefix(path, "/api/firecrawl/sources/") {
			parts := strings.Split(strings.TrimPrefix(path, "/api/firecrawl/sources/"), "/")
			if len(parts) != 2 || parts[1] != "hh" {
				jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "backend_not_migrated"})
				return
			}
			if r.Method != http.MethodPut {
				jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
				return
			}
			legacySource = true
			legacyCompany = parts[0]
			path = "/api/apify/hh-source"
		}
		if path != "/api/apify/settings" && path != "/api/apify/key" && path != "/api/apify/hh-source" {
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Метод API не найден."})
			return
		}
		if (path == "/api/apify/key" && r.Method != http.MethodPut) || (path != "/api/apify/key" && r.Method != http.MethodGet && r.Method != http.MethodPut) {
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if r.Method != http.MethodGet && !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if service == nil {
			apifySettingsError(w, apifysettings.ErrStorage)
			return
		}
		var companyID int64
		if path == "/api/apify/hh-source" {
			params, err := url.ParseQuery(r.URL.RawQuery)
			if legacySource && err == nil && len(params) == 0 {
				params = url.Values{"companyId": {legacyCompany}}
			} else if legacySource {
				apifySettingsError(w, apifysettings.ErrInput)
				return
			}
			if err != nil || len(params) != 1 || len(params["companyId"]) != 1 {
				apifySettingsError(w, apifysettings.ErrInput)
				return
			}
			companyID, err = strconv.ParseInt(params.Get("companyId"), 10, 64)
			if err != nil || companyID < 1 || companyID > 9007199254740991 {
				apifySettingsError(w, apifysettings.ErrInput)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if r.Method == http.MethodGet {
			if path == "/api/apify/hh-source" {
				view, err := service.ReadHHSource(ctx, companyID)
				if err != nil {
					apifySettingsError(w, err)
					return
				}
				jsonReply(w, http.StatusOK, view)
				return
			}
			view, err := service.Read(ctx)
			if err != nil {
				apifySettingsError(w, err)
				return
			}
			jsonReply(w, http.StatusOK, view)
			return
		}
		generation, err := service.Generation(ctx)
		if err != nil {
			apifySettingsError(w, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				jsonReply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "Слишком длинный запрос."})
			} else {
				apifySettingsError(w, apifysettings.ErrInput)
			}
			return
		}
		if path == "/api/apify/hh-source" {
			input, err := apifysettings.ParseHHSource(body)
			if err != nil {
				apifySettingsError(w, err)
				return
			}
			view, err := service.SaveHHSource(ctx, generation, companyID, input)
			if err != nil {
				apifySettingsError(w, err)
				return
			}
			jsonReply(w, http.StatusOK, view)
			return
		}
		var view apifysettings.PublicView
		if path == "/api/apify/key" {
			var input apifysettings.KeyInput
			input, err = apifysettings.ParseKey(body)
			if err == nil {
				view, err = service.SaveKey(ctx, generation, input)
			}
		} else {
			var input apifysettings.SettingsInput
			input, err = apifysettings.ParseSettings(body)
			if err == nil {
				view, err = service.SaveSettings(ctx, generation, input)
			}
		}
		if err != nil {
			apifySettingsError(w, err)
			return
		}
		jsonReply(w, http.StatusOK, view)
	})
}
