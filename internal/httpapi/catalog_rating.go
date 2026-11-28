package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

type CatalogRatingWriter interface {
	RecalculateRating(context.Context) (catalog.RatingResult, error)
}

func catalogRatingHandler(m *auth.Manager, writer CatalogRatingWriter) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(m, r)
		if !user.IsAdmin || r.Method == http.MethodPost && !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору приложения."})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if writer == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище компаний пока не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, err := writer.RecalculateRating(ctx)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось пересчитать рейтинг."})
			return
		}
		jsonReply(w, http.StatusOK, result)
	})
}
