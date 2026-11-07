package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerAISpendingReader interface {
	ReadAISpending(context.Context) (json.RawMessage, error)
}

// Только администратор. GET не обновляет бюджет и не связывается с ИИ.
func SchedulerAISpendingDebugHandler(manager *auth.Manager, reader SchedulerAISpendingReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/scheduler/ai-spending/debug" {
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
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, err := reader.ReadAISpending(ctx)
		if err != nil {
			schedulerSettingsFailure(w, parser.ErrDashboardStorage)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		jsonReply(w, http.StatusOK, body)
	})
}
