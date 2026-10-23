package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type ApplicationReader interface {
	ByID(context.Context, string, int64) (applications.Record, error)
	CRM(context.Context, string) (applications.CRMResponse, error)
}

// ApplicationDetailHandler returns one record and history, never revealing
// whether another account owns a non-existent ID.
func ApplicationDetailHandler(m *auth.Manager, repo ApplicationReader) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID отклика."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		record, err := repo.ByID(ctx, user.Name, id)
		switch {
		case errors.Is(err, applications.ErrNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Отклик не найден."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище откликов временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, map[string]any{"record": record})
		}
	})
}

func ApplicationsCRMHandler(m *auth.Manager, repo ApplicationReader) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		result, err := repo.CRM(ctx, user.Name)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище откликов временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, result)
	})
}
