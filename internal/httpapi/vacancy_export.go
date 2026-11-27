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

type CompanyExporter interface {
	ExportCompaniesXLSX(context.Context) ([]byte, error)
}

func vacancyExportHandler(manager *auth.Manager, exporter VacancyExporter) http.HandlerFunc {
	var read func(context.Context, string) ([]byte, error)
	if exporter != nil {
		read = exporter.ExportXLSX
	}
	return xlsxExportHandler(manager, read, "вакансий", "vacancies")
}

func companyExportHandler(manager *auth.Manager, exporter CompanyExporter) http.HandlerFunc {
	var read func(context.Context, string) ([]byte, error)
	if exporter != nil {
		read = func(ctx context.Context, _ string) ([]byte, error) { return exporter.ExportCompaniesXLSX(ctx) }
	}
	return xlsxExportHandler(manager, read, "компаний", "companies")
}

func xlsxExportHandler(manager *auth.Manager, read func(context.Context, string) ([]byte, error), label, filename string) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		w.Header().Set("Cache-Control", "private, no-store")
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if read == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище " + label + " пока не подключено."})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		file, err := read(ctx, user.Name)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось подготовить выгрузку " + label + "."})
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="it-employers-`+filename+`-`+time.Now().UTC().Format("2006-01-02")+`.xlsx"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(file)))
		_, _ = w.Write(file)
	})
}
