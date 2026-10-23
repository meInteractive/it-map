package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type VacancyLister interface {
	List(context.Context, string, *vacancies.Period, time.Time) (vacancies.ListResponse, error)
}

func VacancyListHandler(m *auth.Manager, repo VacancyLister, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		var period *vacancies.Period
		if _, present := r.URL.Query()["period"]; present {
			p, err := vacancies.ParsePeriod(r.URL.Query().Get("period"))
			// A supplied empty period is invalid in Sites v316; omission is
			// different, and returns all vacancies before the UI filters them.
			if err != nil || r.URL.Query().Get("period") == "" {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный период публикации."})
				return
			}
			period = &p
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		result, err := repo.List(ctx, user.Name, period, now())
		if err != nil {
			// Never expose SQL, connection strings or personal records.
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, result)
	})
}
