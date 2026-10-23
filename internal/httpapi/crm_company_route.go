package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/crm"
)

type CompanyCRMReader interface {
	Company(context.Context, string, string) (crm.CompanyState, error)
}

func CompanyCRMHandler(m *auth.Manager, repo CompanyCRMReader) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("company"))
		if name == "" || len(name) > 255 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректная компания."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		state, err := repo.Company(ctx, user.Name, name)
		switch {
		case errors.Is(err, crm.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище CRM временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, state)
		}
	})
}
