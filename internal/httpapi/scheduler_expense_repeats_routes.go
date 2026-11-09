package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerExpenseRepeatsReader interface {
	ReadExpenseRepeats(context.Context, int) (json.RawMessage, error)
}

// GO-434: admin-only, bounded, SELECT-only local evidence. No provider requests.
func SchedulerExpenseRepeatsHandler(m *auth.Manager, reader SchedulerExpenseRepeatsReader) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(m, r)
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != http.MethodGet {
			if !writeSameOrigin(r) {
				jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
				return
			}
			if _, ok := schedulerRequestInput(w, r); ok {
				jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			}
			return
		}
		if r.URL.EscapedPath() != "/api/scheduler/expense-repeats" {
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Маршрут не найден."})
			return
		}
		days := 30
		for key, values := range r.URL.Query() {
			if key != "days" || len(values) != 1 {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Неизвестные параметры диагностики."})
				return
			}
			value, err := strconv.Atoi(values[0])
			if err != nil {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный период."})
				return
			}
			days = value
		}
		if days != 7 && days != 30 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный период."})
			return
		}
		if reader == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, err := reader.ReadExpenseRepeats(ctx, days)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, parser.ErrExpenseRepeatsInput) {
				status = http.StatusBadRequest
			}
			jsonReply(w, status, map[string]string{"error": "Не удалось получить диагностику повторов."})
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		jsonReply(w, http.StatusOK, body)
	})
}
