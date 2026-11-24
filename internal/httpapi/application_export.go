package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type applicationExporter interface {
	Export(context.Context, string) (applications.ExportData, error)
}

func applicationsExportHandler(manager *auth.Manager, exporter applicationExporter) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		data, err := exporter.Export(ctx, user.Name)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось подготовить выгрузку откликов."})
			return
		}
		jsonReply(w, http.StatusOK, data)
	})
}
