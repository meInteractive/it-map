package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
)

type VacancyExporter interface {
	ExportXLSX(context.Context, string) ([]byte, error)
}

func vacancyExportHandler(manager *auth.Manager, exporter VacancyExporter) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		w.Header().Set("Cache-Control", "private, no-store")
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if exporter == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий пока не подключено."})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		file, err := exporter.ExportXLSX(ctx, user.Name)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось подготовить выгрузку вакансий."})
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="it-employers-vacancies-`+time.Now().UTC().Format("2006-01-02")+`.xlsx"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(file)))
		_, _ = w.Write(file)
	})
}
