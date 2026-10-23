package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/auth"
	"itmap/internal/crm"
)

type CompanyFlagWriter interface {
	SetCompanyFlag(context.Context, string, string, string, bool) (crm.CompanyState, error)
}

// CompanyFlagWriteHandler keeps the original PUT /api/crm/favorite and
// /api/crm/hidden-company contract. It uses the shared strict JSON and
// same-origin guards to prevent duplicate/unknown fields from slipping in.
func CompanyFlagWriteHandler(m *auth.Manager, repo CompanyFlagWriter, flag string) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		company := strings.TrimSpace(r.URL.Query().Get("company"))
		if company == "" || utf8.RuneCountInString(company) > 255 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректная компания."})
			return
		}
		key, value, err := parseFlagUpdate(w, r)
		if err != nil || key != flag {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректное значение признака компании."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		state, err := repo.SetCompanyFlag(ctx, user.Name, company, flag, value)
		switch {
		case errors.Is(err, crm.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case errors.Is(err, crm.ErrAmbiguousState):
			jsonReply(w, http.StatusConflict, map[string]string{"error": "Обнаружены дублирующиеся записи CRM. Требуется проверка базы данных."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось сохранить признаки компании."})
		default:
			jsonReply(w, http.StatusOK, state)
		}
	})
}
