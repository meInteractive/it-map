package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type VacancyDetailer interface {
	DetailByID(context.Context, string, int64) (vacancies.DetailResponse, error)
}

func VacancyDetailHandler(m *auth.Manager, repo VacancyDetailer) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID вакансии."})
			return
		}
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		detail, err := repo.DetailByID(ctx, user.Name, id)
		switch {
		case errors.Is(err, vacancies.ErrNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Вакансия не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище вакансий временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, detail)
		}
	})
}
