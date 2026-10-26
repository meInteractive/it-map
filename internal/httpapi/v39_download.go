package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
)

type BackupExporter interface {
	Export(context.Context) (backup.Download, error)
}

func BackupDownloadHandler(manager *auth.Manager, store BackupExporter) http.HandlerFunc {
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
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище данных пока не подключено."})
			return
		}
		if r.Method == "POST" {
			if !writeSameOrigin(r) {
				jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
				return
			}
			jsonReply(w, 503, map[string]string{"error": "backend_not_migrated"})
			return
		}
		if r.Method != "GET" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		file, err := store.Export(ctx)
		if err != nil {
			var input *backup.InputError
			if errors.As(err, &input) {
				jsonReply(w, 400, map[string]string{"error": input.Message})
			} else {
				jsonReply(w, 503, map[string]string{"error": "Не удалось создать полный бэкап."})
			}
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Disposition", `attachment; filename="it-employers-backup-`+file.CreatedAt.UTC().Format("2006-01-02")+`.json"`)
		w.WriteHeader(200)
		_, _ = w.Write(file.Bytes)
	})
}
