package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerDashboardReader interface {
	ReadDashboard(context.Context) (json.RawMessage, error)
}

func SchedulerDashboardHandler(manager *auth.Manager, store SchedulerDashboardReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		if r.Method != "GET" && !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if r.Method != "GET" || r.URL.EscapedPath() != "/api/scheduler" {
			if _, ok := schedulerRequestInput(w, r); ok {
				jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		body, e := store.ReadDashboard(ctx)
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		jsonReply(w, 200, body)
	})
}

// Исходный роутер читает ограниченное тело до ответа о неподдерживаемом методе.
func schedulerRequestInput(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	if r.ContentLength > 32768 {
		schedulerInputTooLarge(w)
		return nil, false
	}
	if r.Body == nil {
		schedulerSettingsFailure(w, parser.ErrSettingsStorage)
		return nil, false
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if e != nil {
		var large *http.MaxBytesError
		if errors.As(e, &large) {
			schedulerInputTooLarge(w)
		} else {
			schedulerSettingsFailure(w, e)
		}
		return nil, false
	}
	// TextDecoder удаляет начальный UTF-8 BOM. encoding/json также заменяет
	// некорректный UTF-8 внутри строк, как декодер исходного роутера.
	body = bytes.Trim(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}), " \t\r\n")
	if !json.Valid(body) || len(body) == 0 || body[0] != '{' {
		schedulerSettingsFailure(w, parser.ErrSettingsStorage)
		return nil, false
	}
	return body, true
}
