package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/vacancies"
)

type VacancyLister interface {
	List(context.Context, string, *vacancies.Period, time.Time) (vacancies.ListResponse, error)
}

// URLSearchParams.get сохраняет первый period и не отбрасывает пары с semicolon.
func vacancyPeriodQuery(raw string) (string, bool) {
	for _, pair := range strings.Split(raw, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if backup.CatalogQueryValue("q="+key, "q") == "period" {
			return backup.CatalogQueryValue(pair, "period"), true
		}
	}
	return "", false
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
		if r.URL.EscapedPath() != "/api/vacancies/list" {
			w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not found"))
			return
		}
		if repo == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий пока не подключено."})
			return
		}
		var period *vacancies.Period
		if value, present := vacancyPeriodQuery(r.URL.RawQuery); present {
			p, err := vacancies.ParsePeriod(value)
			// A supplied empty period is invalid in Sites v316; omission is
			// different, and returns all vacancies before the UI filters them.
			if err != nil || value == "" {
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
