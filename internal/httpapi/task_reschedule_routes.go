package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/tasks"
)

// TaskRescheduler is defined at the HTTP boundary to keep SQL out of handlers.
type TaskRescheduler interface {
	Reschedule(context.Context, string, int64, string) (string, error)
}

var taskDeadlinePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$`)

// parseTaskUpdate enforces the existing frontend's mutually exclusive task
// updates and rejects duplicate JSON keys, unknown fields and trailing input.
func parseTaskUpdate(w http.ResponseWriter, r *http.Request) (key string, completed bool, dueAt string, err error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return "", false, "", errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return "", false, "", errors.New("expected object")
	}
	tok, e = d.Token()
	if e != nil {
		return "", false, "", errors.New("missing field")
	}
	key, ok := tok.(string)
	if !ok {
		return "", false, "", errors.New("invalid field")
	}
	switch key {
	case "completed":
		if e = d.Decode(&completed); e != nil {
			return "", false, "", errors.New("invalid completion")
		}
	case "dueAt":
		if e = d.Decode(&dueAt); e != nil || !taskDeadlinePattern.MatchString(dueAt) {
			return "", false, "", errors.New("invalid deadline")
		}
		if _, e = time.Parse(time.RFC3339Nano, dueAt); e != nil {
			return "", false, "", errors.New("invalid deadline")
		}
	default:
		return "", false, "", errors.New("unknown field")
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') {
		return "", false, "", errors.New("unexpected fields")
	}
	var trailing any
	if e = d.Decode(&trailing); e != io.EOF {
		return "", false, "", errors.New("trailing input")
	}
	return key, completed, dueAt, nil
}

// TaskMutationHandler extends the already ported completion endpoint with
// rescheduling while deliberately refusing calendar-linked edits.
func TaskMutationHandler(m *auth.Manager, completion TaskCompletionWriter, reschedule TaskRescheduler) http.HandlerFunc {
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
		key, value, dueAt, err := parseTaskUpdate(w, r)
		if err != nil {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректные данные задачи."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if key == "completed" {
			if completion == nil {
				jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Метод пока недоступен."})
				return
			}
			err = completion.SetCompleted(ctx, user.Name, id, value)
		} else {
			if reschedule == nil {
				jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Метод пока недоступен."})
				return
			}
			dueAt, err = reschedule.Reschedule(ctx, user.Name, id, dueAt)
		}
		switch {
		case errors.Is(err, tasks.ErrTaskNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Задача не найдена."})
		case errors.Is(err, tasks.ErrCalendarSyncRequired):
			jsonReply(w, http.StatusConflict, map[string]string{"error": "Для изменения срока сначала необходимо перенести синхронизацию Google Calendar."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось обновить задачу."})
		case key == "dueAt":
			jsonReply(w, http.StatusOK, map[string]any{"ok": true, "dueAt": dueAt, "calendarSync": map[string]string{"status": "not_connected"}})
		default:
			jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
		}
	})
}
