package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/tasks"
)

type TaskCompletionWriter interface {
	SetCompleted(context.Context, string, int64, bool) error
}

// parseCompletion permits exactly one JSON key and one boolean value, preventing
// duplicate keys and unnoticed changes to linked calendar/task fields.
func parseCompletion(w http.ResponseWriter, r *http.Request) (bool, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false, errors.New("expected JSON content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false, errors.New("expected object")
	}
	token, err = d.Token()
	if err != nil || token != "completed" {
		return false, errors.New("expected completed key")
	}
	var value bool
	if err = d.Decode(&value); err != nil {
		return false, err
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return false, errors.New("unexpected key")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return false, errors.New("trailing input")
	}
	return value, nil
}

func TaskCompletionHandler(m *auth.Manager, repo TaskCompletionWriter) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID задачи."})
			return
		}
		completed, err := parseCompletion(w, r)
		if err != nil {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный статус задачи."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err = repo.SetCompleted(ctx, user.Name, id, completed)
		switch {
		case errors.Is(err, tasks.ErrTaskNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Задача не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось обновить задачу."})
		default:
			jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
		}
	})
}
