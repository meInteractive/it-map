package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/tasks"
)

type TaskLister interface {
	List(context.Context, string, string, bool, *int64) (tasks.Response, error)
}

func TaskListHandler(manager *auth.Manager, repo TaskLister) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		query := r.URL.Query()
		company := strings.TrimSpace(query.Get("company"))
		if len(company) > 255 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректное название компании."})
			return
		}
		var vacancyID *int64
		if raw := query.Get("vacancy"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id < 1 {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректная вакансия."})
				return
			}
			vacancyID = &id
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		data, err := repo.List(ctx, user.Name, company, query.Get("includeCompleted") == "1", vacancyID)
		switch {
		case errors.Is(err, tasks.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище задач временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, data)
		}
	})
}
