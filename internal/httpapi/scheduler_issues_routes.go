package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"itmap/internal/auth"
)

// SchedulerIssuesReader не предоставляет доступ к платным операциям или записи.
type SchedulerIssuesReader interface {
	ReadIssues(context.Context) (json.RawMessage, error)
}

type SchedulerIssueSampler interface {
	Sample(context.Context) error
}

type SchedulerIssuesStore interface {
	SchedulerIssuesReader
	SchedulerIssueSampler
}

func SchedulerIssuesHandler(manager *auth.Manager, store SchedulerIssuesReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		if r.Method != http.MethodGet {
			if !writeSameOrigin(r) {
				jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
				return
			}
			if _, ok := schedulerRequestInput(w, r); ok {
				jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			}
			return
		}
		if r.URL.EscapedPath() != "/api/scheduler/issues" {
			jsonReply(w, 404, map[string]string{"error": "Маршрут не найден."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, err := store.ReadIssues(ctx)
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		jsonReply(w, 200, body)
	})
}
