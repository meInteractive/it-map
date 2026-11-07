package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/auth"
)

type ApifyOperationsDebugReader interface {
	ReadOperationsDebug(context.Context) (apifyruns.OperationsDebugView, error)
}

// Только защищённое GET чтение локального журнала Actor, без обновления
// provider usage и без POST к Apify.
func ApifyOperationsDebugHandler(manager *auth.Manager, reader ApifyOperationsDebugReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/apify/operations/debug" {
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if r.URL.RawQuery != "" {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Неизвестные параметры запроса."})
			return
		}
		if reader == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		view, err := reader.ReadOperationsDebug(ctx)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось прочитать операции Apify."})
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		jsonReply(w, http.StatusOK, view)
	})
}
