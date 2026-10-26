package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type ResumeStore interface {
	ReadResume(context.Context, string) (profiles.Resume, error)
	SaveResume(context.Context, string, profiles.TextInput) (profiles.Resume, error)
}

func profileInputBody(w http.ResponseWriter, r *http.Request, limit int) ([]byte, error) {
	if r.ContentLength > int64(limit) {
		return nil, &profiles.InputError{Status: 413, Message: "Запрос слишком большой."}
	}
	if r.Body == nil {
		return nil, &profiles.InputError{Status: 400, Message: "Передайте данные профиля."}
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(limit))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return nil, &profiles.InputError{Status: 413, Message: "Запрос слишком большой."}
		}
		return nil, &profiles.InputError{Status: 400, Message: "Некорректные данные."}
	}
	return body, nil
}
func profileInputError(w http.ResponseWriter, err error) {
	var input *profiles.InputError
	if errors.As(err, &input) {
		jsonReply(w, input.Status, map[string]string{"error": input.Message})
		return
	}
	jsonReply(w, 400, map[string]string{"error": "Некорректные данные профиля."})
}
func ProfileResumeHandler(manager *auth.Manager, store ResumeStore) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		status := profiles.AccessStatus("resume", "", r.Method, true, user.IsAdmin, store != nil)
		if status == 405 {
			w.Header().Set("Allow", "GET, PUT")
			jsonReply(w, status, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if status != 200 {
			jsonReply(w, status, map[string]string{"error": "Хранилище резюме пока не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if r.Method == "GET" {
			result, err := store.ReadResume(ctx, user.Name)
			if err != nil {
				jsonReply(w, 503, map[string]string{"error": "Не удалось прочитать резюме."})
				return
			}
			jsonReply(w, 200, result)
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		body, err := profileInputBody(w, r, profiles.ResumeBodyLimit)
		if err != nil {
			profileInputError(w, err)
			return
		}
		input, err := profiles.ParseResume(body, r.ContentLength)
		if err != nil {
			profileInputError(w, err)
			return
		}
		saved, err := store.SaveResume(ctx, user.Name, input)
		switch {
		case errors.Is(err, profiles.ErrConflict):
			jsonReply(w, 409, map[string]string{"error": "Резюме изменено в другой вкладке. Скопируйте свой текст и обновите страницу перед повторным сохранением."})
		case err != nil:
			var invalid *profiles.InputError
			if errors.As(err, &invalid) {
				profileInputError(w, err)
				return
			}
			jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить резюме."})
		default:
			jsonReply(w, 200, saved)
		}
	})
}
