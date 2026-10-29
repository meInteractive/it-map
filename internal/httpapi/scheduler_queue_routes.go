package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/parser"
)

type SchedulerQueueReader interface {
	PrepareQueue(context.Context) (func(context.Context, json.RawMessage) (json.RawMessage, error), error)
}

func SchedulerQueueHandler(manager *auth.Manager, store SchedulerQueueReader) http.HandlerFunc {
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
		if r.Method != "GET" && !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		captureCtx, captureCancel := context.WithTimeout(r.Context(), 35*time.Second)
		read, e := store.PrepareQueue(captureCtx)
		captureCancel()
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		if read == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		if r.Method != "GET" || r.URL.EscapedPath() != "/api/scheduler/queue" {
			if _, ok := schedulerRequestInput(w, r); ok {
				jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, e := read(ctx, schedulerQueryObject(r.URL.RawQuery))
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		jsonReply(w, 200, body)
	})
}

// Object.fromEntries(URLSearchParams) сохраняет последнее значение дубликата.
func schedulerQueryObject(raw string) json.RawMessage {
	values := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key = backup.CatalogQueryValue("q="+key, "q")
		value = backup.CatalogQueryValue("q="+value, "q")
		values[key] = value
	}
	result, _ := json.Marshal(values)
	return result
}
