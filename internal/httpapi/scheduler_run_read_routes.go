package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/parser"
)

type SchedulerRunReader interface {
	PrepareRunRead(context.Context) (func(context.Context, json.RawMessage) (json.RawMessage, error), error)
}

func SchedulerRunReadHandler(manager *auth.Manager, store SchedulerRunReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			jsonReply(w, 503, map[string]string{"error": "backend_not_migrated"})
			return
		}
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		read, e := store.PrepareRunRead(ctx)
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		if read == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		body, e := read(ctx, schedulerRunQuery(r.URL.RawQuery))
		if e != nil {
			if errors.Is(e, parser.ErrRunReadNotFound) {
				jsonReply(w, 404, map[string]string{"error": "Сохранённый запуск не найден.", "code": "not_found"})
			} else {
				schedulerSettingsFailure(w, e)
			}
			return
		}
		jsonReply(w, 200, body)
	})
}

// checkKeys использует Object.fromEntries, а requestId затем берётся через get(first).
func schedulerRunQuery(raw string) json.RawMessage {
	var values map[string]json.RawMessage
	_ = json.Unmarshal(schedulerQueryObject(raw), &values)
	if _, ok := values["requestId"]; ok {
		values["requestId"], _ = json.Marshal(backup.CatalogQueryValue(raw, "requestId"))
	}
	out, _ := json.Marshal(values)
	return out
}
