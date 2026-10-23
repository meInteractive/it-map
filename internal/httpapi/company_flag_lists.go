package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/auth"
)

type CompanyFlagLister interface {
	CompanyNames(context.Context, string, string) ([]string, error)
}

func CompanyFlagListHandler(m *auth.Manager, repo CompanyFlagLister, flag string) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		names, err := repo.CompanyNames(ctx, user.Name, flag)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище CRM временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"companyNames": names})
	})
}
