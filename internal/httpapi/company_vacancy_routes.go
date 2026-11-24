package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/vacancies"
)

type CompanyVacancyLister interface {
	ListForCompany(context.Context, string, string) (vacancies.CompanyListResponse, error)
}

func companyVacanciesHandler(m *auth.Manager, repo CompanyVacancyLister, fallback http.HandlerFunc) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		company := strings.TrimSpace(backup.CatalogQueryValue(r.URL.RawQuery, "company"))
		if company == "" || backup.CatalogQueryValue(r.URL.RawQuery, "channel") != "" {
			fallback(w, r)
			return
		}
		user, _ := sessionFromRequest(m, r)
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		result, err := repo.ListForCompany(ctx, user.Name, company)
		switch {
		case errors.Is(err, vacancies.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, result)
		}
	})
}
