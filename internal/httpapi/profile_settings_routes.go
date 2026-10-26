package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type ProfileSettingsStore interface {
	ReadPrompts(context.Context) (profiles.PromptCollection, error)
	SavePrompt(context.Context, string, profiles.TextInput) (profiles.Prompt, error)
	ReadModels(context.Context) (map[string]profiles.Model, error)
	SaveModel(context.Context, string, profiles.ModelInput) (profiles.Model, error)
}

// Availability/capabilities are a separate dependency from syntax/storage. A
// missing validator never silently accepts OpenAI or JEV choices. Runtime does
// not wire provider catalogs in this migration slice; tests supply memory mocks.
type ProfileModelValidator interface {
	ValidateChoice(context.Context, string, profiles.ModelInput) error
}

func ProfileSettingsHandler(manager *auth.Manager, store ProfileSettingsStore, catalog ProfileModelValidator, surface string) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		prefix := "/api/profile/prompts"
		if surface == "models" {
			prefix = "/api/profile/prompt-models"
		}
		path := r.URL.EscapedPath()
		kind := ""
		if path != prefix {
			kind = strings.TrimPrefix(path, prefix+"/")
			if kind == "" {
				kind = "/" // A trailing slash is an unknown item in the legacy route.
			}
		}
		status := profiles.AccessStatus(surface, kind, r.Method, true, user.IsAdmin, store != nil)
		if status != 200 {
			message := "Промт не найден."
			switch status {
			case 403:
				message = "Доступно только администратору."
			case 503:
				message = "Хранилище промтов пока не подключено."
				if surface == "models" {
					message = "Хранилище не подключено."
				}
			case 405:
				message = "Метод не поддерживается."
				w.Header().Set("Allow", "PUT")
			}
			jsonReply(w, status, map[string]string{"error": message})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		if r.Method == "GET" {
			if surface == "prompts" {
				rows, err := store.ReadPrompts(ctx)
				if err != nil {
					jsonReply(w, 503, map[string]string{"error": "Не удалось прочитать промты."})
					return
				}
				jsonReply(w, 200, rows)
			} else {
				rows, err := store.ReadModels(ctx)
				if err != nil {
					jsonReply(w, 503, map[string]string{"error": "Не удалось прочитать настройки моделей."})
					return
				}
				jsonReply(w, 200, map[string]any{"models": rows})
			}
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		limit := profiles.PromptBodyLimit(kind)
		if surface == "models" {
			limit = 4096
		}
		body, err := profileInputBody(w, r, limit)
		if err != nil {
			profileInputError(w, err)
			return
		}
		if surface == "prompts" {
			input, err := profiles.ParsePrompt(kind, body, r.ContentLength)
			if err != nil {
				profileInputError(w, err)
				return
			}
			saved, err := store.SavePrompt(ctx, kind, input)
			if err != nil {
				sharedSettingError(w, err, kind, false)
				return
			}
			jsonReply(w, 200, saved)
			return
		}
		input, err := profiles.ParseModel(kind, body, r.ContentLength)
		if err != nil {
			profileInputError(w, err)
			return
		}
		if input.Provider == "openai" || input.Provider == "openrouter" && input.OpenRouterModel == "jev" {
			if catalog == nil {
				jsonReply(w, 503, map[string]string{"error": "Не удалось проверить доступность выбранной модели."})
				return
			}
			if err = catalog.ValidateChoice(ctx, kind, input); err != nil {
				var invalid *profiles.InputError
				if errors.As(err, &invalid) {
					profileInputError(w, err)
				} else {
					jsonReply(w, 503, map[string]string{"error": "Не удалось проверить доступность выбранной модели."})
				}
				return
			}
		}
		saved, err := store.SaveModel(ctx, kind, input)
		if err != nil {
			sharedSettingError(w, err, kind, true)
			return
		}
		jsonReply(w, 200, saved)
	})
}
func sharedSettingError(w http.ResponseWriter, err error, kind string, model bool) {
	if errors.Is(err, profiles.ErrConflict) {
		message := "Настройки изменены в другой вкладке. Обновите страницу."
		if kind == "cover_letter" || kind == "relevance" {
			if model {
				message = "Настройки модели изменены в другой вкладке. Обновите страницу."
			} else {
				message = "Промт изменён в другой вкладке. Скопируйте текст и обновите страницу перед повторным сохранением."
			}
		}
		jsonReply(w, 409, map[string]string{"error": message})
		return
	}
	var invalid *profiles.InputError
	if errors.As(err, &invalid) {
		profileInputError(w, err)
		return
	}
	jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить настройки."})
}
