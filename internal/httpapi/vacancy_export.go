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

type CompanySourceExporter interface {
	ExportVacancySourcesXLSX(context.Context, string, bool) ([]byte, error)
	ExportReviewSourcesXLSX(context.Context, string) ([]byte, error)
}

func companyVacancySourcesExportHandler(manager *auth.Manager, exporter CompanySourceExporter) http.HandlerFunc {
	var read func(*http.Request, string) ([]byte, string, error)
	if exporter != nil {
		read = func(r *http.Request, user string) ([]byte, string, error) {
			unranked := r.URL.Query().Get("unranked") == "1"
			filename := "vacancy-sources"
			if unranked {
				filename += "-unranked"
			}
			file, err := exporter.ExportVacancySourcesXLSX(r.Context(), user, unranked)
			return file, filename, err
		}
	}
	return xlsxDownloadHandler(manager, read, "источников вакансий")
}

func companyReviewSourcesExportHandler(manager *auth.Manager, exporter CompanySourceExporter) http.HandlerFunc {
	var read func(context.Context, string) ([]byte, error)
	if exporter != nil {
		read = exporter.ExportReviewSourcesXLSX
	}
	return xlsxExportHandler(manager, read, "источников отзывов", "review-sources")
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
	var download func(*http.Request, string) ([]byte, string, error)
	if read != nil {
		download = func(r *http.Request, user string) ([]byte, string, error) {
			file, err := read(r.Context(), user)
			return file, filename, err
		}
	}
	return xlsxDownloadHandler(manager, download, label)
}

func xlsxDownloadHandler(manager *auth.Manager, read func(*http.Request, string) ([]byte, string, error), label string) http.HandlerFunc {
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
		file, filename, err := read(r.WithContext(ctx), user.Name)
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
