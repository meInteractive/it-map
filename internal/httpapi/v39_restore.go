package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
)

type BackupRestorer interface {
	ValidateV39Restore(context.Context, []byte) (backup.RestoreValidation, error)
	RestoreV39(context.Context, []byte) (backup.RestoreResult, error)
}

func backupRestoreError(w http.ResponseWriter, e error) {
	var input *backup.InputError
	if errors.As(e, &input) {
		jsonReply(w, input.Status, map[string]string{"error": input.Message})
		return
	}
	jsonReply(w, 400, map[string]string{"error": "Не удалось восстановить данные."})
}
func BackupRestoreHandler(manager *auth.Manager, store BackupRestorer, validate bool) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != "POST" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище данных пока не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		data, e := backup.ReadRequest(r.Body, r.ContentLength)
		if e != nil {
			backupRestoreError(w, e)
			return
		}
		if validate {
			result, e := store.ValidateV39Restore(ctx, data)
			if e != nil {
				backupRestoreError(w, e)
				return
			}
			jsonReply(w, 200, map[string]any{"ok": true, "tables": result.Tables, "rows": result.Rows, "warnings": result.Warnings})
			return
		}
		result, e := store.RestoreV39(ctx, data)
		if e != nil {
			backupRestoreError(w, e)
			return
		}
		// Preserve source summary including its optional loose agencies.length value.
		companies, agencies := backup.RestoreCatalogCounts(result.Catalog)
		jsonReply(w, 200, map[string]any{"ok": true, "sourceVersion": 39, "version": 39, "companies": companies, "agencies": agencies, "tables": 52, "rows": result.Rows, "warnings": result.Warnings})
	})
}
