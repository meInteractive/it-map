package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type namedResumeStore interface {
	ReadResumes(context.Context, string) (profiles.Resumes, error)
	SaveResumes(context.Context, string, profiles.Resumes) (profiles.Resumes, error)
}

func profileResumesHandler(manager *auth.Manager, store namedResumeStore) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		var result profiles.Resumes
		var err error
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище резюме недоступно."})
			return
		}
		switch r.Method {
		case http.MethodGet:
			result, err = store.ReadResumes(ctx, user.Name)
			if r.URL.Query().Get("view") == "options" {
				for i := range result.Items {
					result.Items[i].Markdown = ""
				}
			}
		case http.MethodPut:
			if !writeSameOrigin(r) {
				jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
				return
			}
			body, err := profileInputBody(w, r, 768<<10)
			if err != nil {
				profileInputError(w, err)
				return
			}
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			var input profiles.Resumes
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
				jsonReply(w, 400, map[string]string{"error": "Некорректные данные резюме."})
				return
			}
			saved, saveErr := store.SaveResumes(ctx, user.Name, input)
			result = saved
			if saveErr != nil {
				writeResumesError(w, saveErr)
				return
			}
		default:
			w.Header().Set("Allow", "GET, PUT")
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if err != nil {
			writeResumesError(w, err)
			return
		}
		jsonReply(w, 200, result)
	})
}

func writeResumesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, profiles.ErrConflict):
		jsonReply(w, 409, map[string]string{"error": "Резюме изменились в другой вкладке. Обновите страницу перед сохранением."})
	case errors.Is(err, profiles.ErrResumeSelection):
		jsonReply(w, 400, map[string]string{"error": "Проверьте название, текст и выбор основного резюме."})
	default:
		jsonReply(w, 503, map[string]string{"error": "Хранилище резюме временно недоступно."})
	}
}
