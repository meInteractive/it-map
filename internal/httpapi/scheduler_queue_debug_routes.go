package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

// Новый endpoint существует отдельно от старого /queue, чтобы не менять его
// контракт и не отдавать в JSON-диагностику приватные служебные поля.
type SchedulerQueueDebugReader interface {
	PrepareQueueDebug(context.Context) (func(context.Context, json.RawMessage) (json.RawMessage, error), error)
}

func SchedulerQueueDebugHandler(manager *auth.Manager, store SchedulerQueueDebugReader) http.HandlerFunc {
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
		if r.Method != "GET" || r.URL.EscapedPath() != "/api/scheduler/queue/debug" {
			if _, ok := schedulerRequestInput(w, r); ok {
				jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			}
			return
		}
		capture, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		read, err := store.PrepareQueueDebug(capture)
		cancel()
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		if read == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, err := read(ctx, schedulerQueryObject(r.URL.RawQuery))
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		jsonReply(w, 200, body)
	})
}
