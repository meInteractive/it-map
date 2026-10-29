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

type SchedulerSettingsWriter interface {
	PrepareSave(context.Context, string) (func(context.Context, json.RawMessage) (json.RawMessage, error), error)
}

// Configuration is independent of execution: this handler cannot start a worker
// or call a provider, even when enabled is saved as true.
func SchedulerSettingsHandler(manager *auth.Manager, store SchedulerSettingsWriter) http.HandlerFunc {
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
		// Source candidateDb фиксирует поколение до размера, разбора и fallback метода.
		// Подготовка освобождает соединение до первого чтения тела запроса.
		captureCtx, captureCancel := context.WithTimeout(r.Context(), 35*time.Second)
		save, e := store.PrepareSave(captureCtx, user.Name)
		captureCancel()
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		if save == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		// Source reads the body before its method fallback, and does not require
		// application/json. Preserve its 413 and generic 500 parse failures.
		if r.ContentLength > 32768 {
			schedulerInputTooLarge(w)
			return
		}
		if r.Body == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		if e != nil {
			var large *http.MaxBytesError
			if errors.As(e, &large) {
				schedulerInputTooLarge(w)
			} else {
				schedulerSettingsFailure(w, e)
			}
			return
		}
		// TextDecoder consumes one initial UTF-8 BOM. encoding/json also replaces
		// malformed UTF-8 inside strings, as the source decoder does.
		body = bytes.Trim(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}), " \t\r\n")
		if !json.Valid(body) || len(body) == 0 || body[0] != '{' {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		if r.Method != "PUT" || r.URL.EscapedPath() != "/api/scheduler/settings" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		saved, e := save(ctx, body)
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		jsonReply(w, 200, saved)
	})
}

func schedulerInputTooLarge(w http.ResponseWriter) {
	jsonReply(w, 413, map[string]string{"error": "Запрос слишком большой.", "code": "storage"})
}
func schedulerSettingsFailure(w http.ResponseWriter, e error) {
	status, code, message := 500, "storage", "Не удалось выполнить операцию планировщика."
	var input *parser.SettingsError
	switch {
	case errors.As(e, &input):
		status, code, message = 400, "input", input.Message
	case errors.Is(e, parser.ErrSettingsChanged):
		status, code, message = 409, "changed", "Настройки планировщика изменились. Обновите страницу."
	case errors.Is(e, parser.ErrSettingsGeneration):
		status, code, message = 409, "restored", "База восстановлена во время операции. Старый результат не сохранён. Обновите страницу и повторите запрос."
	}
	jsonReply(w, status, map[string]string{"error": message, "code": code})
}
