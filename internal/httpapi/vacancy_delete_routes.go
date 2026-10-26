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

type ManualVacancyDeleter interface {
	DeleteManual(context.Context, int64) (vacancies.DeleteResult, error)
}

// ManualVacancyDeleteHandler exposes the v316 admin-only DELETE contract.
func ManualVacancyDeleteHandler(m *auth.Manager, repo ManualVacancyDeleter) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID вакансии."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		result, err := repo.DeleteManual(ctx, id)
		switch {
		case errors.Is(err, vacancies.ErrAgencySalaryProjectionPending):
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Удаление вакансии агентства временно недоступно до переноса пересчёта зарплат."})
		case errors.Is(err, vacancies.ErrNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Вакансия не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось удалить вакансию."})
		default:
			if result.Archived {
				jsonReply(w, http.StatusOK, map[string]any{"ok": result.OK, "archived": true, "companyName": result.CompanyName})
				return
			}
			jsonReply(w, http.StatusOK, result)
		}
	})
}
