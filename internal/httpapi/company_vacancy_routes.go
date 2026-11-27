package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/vacancies"
)

type CompanyVacancyLister interface {
	ListForCompany(context.Context, string, string) (vacancies.CompanyListResponse, error)
}

type ChannelVacancyLister interface {
	ListForChannel(context.Context, string, int64) (vacancies.ChannelListResponse, error)
}

func companyVacanciesHandler(m *auth.Manager, repo CompanyVacancyLister, channels ChannelVacancyLister, fallback http.HandlerFunc) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(m, r)
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if raw := backup.CatalogQueryValue(r.URL.RawQuery, "channel"); raw != "" && channels != nil {
			id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if err != nil || id <= 0 || id > 9007199254740991 {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID канала."})
				return
			}
			result, err := channels.ListForChannel(ctx, user.Name, id)
			if err != nil {
				jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий временно недоступно."})
				return
			}
			jsonReply(w, http.StatusOK, result)
			return
		}
		company := strings.TrimSpace(backup.CatalogQueryValue(r.URL.RawQuery, "company"))
		if repo == nil || company == "" || backup.CatalogQueryValue(r.URL.RawQuery, "channel") != "" {
			fallback(w, r)
			return
		}
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
