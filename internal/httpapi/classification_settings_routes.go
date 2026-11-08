package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/auth"
	"itmap/internal/classification"
)

type ClassificationSettingsStore interface {
	Read(context.Context) (classification.SettingsView, error)
	SaveEnabled(context.Context, int64, int64, bool) (classification.SavedRole, error)
	SavePrompt(context.Context, int64, int64, string) (classification.SavedRole, error)
}

func classificationFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, classification.ErrInput):
		jsonReply(w, 400, map[string]string{"error": "Некорректные настройки классификации."})
	case errors.Is(err, classification.ErrConflict):
		jsonReply(w, 409, map[string]string{"error": "Роль или настройки изменились. Скопируйте текст и обновите страницу."})
	default:
		// Технические ошибки, SQL и значения промтов не раскрываются клиенту.
		jsonReply(w, 503, map[string]string{"error": "Настройки классификации недоступны."})
	}
}

func classificationRoleID(path, surface string) (int64, bool) {
	prefix := "/api/classification/" + surface + "/"
	if !strings.HasPrefix(path, prefix) {
		return 0, false
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" || strings.Contains(id, "/") || len(id) > 16 || id[0] == '0' {
		return 0, false
	}
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(id, 10, 64)
	return value, err == nil && value > 0 && value <= classification.MaxSafeRevision
}

// Отдельный ограниченный JSON-контракт; не принимает неизвестные поля
// и не читает неограниченное тело даже при chunked запросе.
func classificationBody(w http.ResponseWriter, r *http.Request, surface string) (int64, *bool, *string, error) {
	if r.Body == nil {
		return 0, nil, nil, classification.ErrInput
	}
	var data map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	if err := dec.Decode(&data); err != nil {
		return 0, nil, nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF || data == nil {
		return 0, nil, nil, classification.ErrInput
	}
	expected := "enabled"
	if surface == "prompts" { expected = "markdown" }
	if len(data) != 2 || data["revision"] == nil || data[expected] == nil {
		return 0, nil, nil, classification.ErrInput
	}
	var revision int64
	if err := json.Unmarshal(data["revision"], &revision); err != nil || revision < 0 || revision >= classification.MaxSafeRevision {
		return 0, nil, nil, classification.ErrInput
	}
	if surface == "roles" {
		var enabled bool
		if err := json.Unmarshal(data["enabled"], &enabled); err != nil || string(data["enabled"]) == "null" {
			return 0, nil, nil, classification.ErrInput
		}
		return revision, &enabled, nil, nil
	}
	var markdown string
	if err := json.Unmarshal(data["markdown"], &markdown); err != nil || string(data["markdown"]) == "null" || !utf8.ValidString(markdown) || utf8.RuneCountInString(markdown) > classification.MaxPromptLength {
		return 0, nil, nil, classification.ErrInput
	}
	return revision, nil, &markdown, nil
}

// surface settings|roles|prompts; намеренно не переносит платный /test.
func ClassificationSettingsHandler(manager *auth.Manager, store ClassificationSettingsStore, surface string) http.HandlerFunc {
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
		if surface == "settings" {
			if path != "/api/classification/settings" {
				jsonReply(w, 404, map[string]string{"error": "Метод API не найден."})
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
				return
			}
		} else if r.Method != http.MethodPut {
			w.Header().Set("Allow", "PUT")
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		var id int64
		if surface != "settings" {
			var valid bool
			id, valid = classificationRoleID(path, surface)
			if !valid {
				jsonReply(w, 404, map[string]string{"error": "Роль не найдена."})
				return
			}
			if !writeSameOrigin(r) {
				jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
				return
			}
		}
		if store == nil {
			classificationFailure(w, classification.ErrStorage)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if surface == "settings" {
			result, err := store.Read(ctx)
			if err != nil { classificationFailure(w, err); return }
			jsonReply(w, 200, result)
			return
		}
		revision, enabled, markdown, err := classificationBody(w, r, surface)
		if err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				jsonReply(w, 413, map[string]string{"error": "Запрос слишком большой."})
			} else {
				classificationFailure(w, classification.ErrInput)
			}
			return
		}
		var saved classification.SavedRole
		if enabled != nil { saved, err = store.SaveEnabled(ctx, id, revision, *enabled) } else { saved, err = store.SavePrompt(ctx, id, revision, *markdown) }
		if err != nil { classificationFailure(w, err); return }
		jsonReply(w, 200, saved)
	})
}
