package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerJobActionWriter interface {
	PrepareJobAction(context.Context) (func(context.Context, string, string) error, error)
}

var schedulerJobActionPath = regexp.MustCompile(`^/api/scheduler/jobs/([0-9a-f-]{36})/(retry|cancel)$`)

// Команды меняют только состояние очереди; выполнение заданий здесь не запускается.
func SchedulerJobActionsHandler(manager *auth.Manager, store SchedulerJobActionWriter) http.HandlerFunc {
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
		save, e := store.PrepareJobAction(captureCtx)
		captureCancel()
		if e != nil {
			schedulerSettingsFailure(w, e)
			return
		}
		if save == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		// Как в source: читаем body до fallback метода, Content-Type не ограничен.
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
		// Source TextDecoder удаляет один начальный UTF-8 BOM.
		body = bytes.Trim(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}), " \t\r\n")
		if !json.Valid(body) || len(body) == 0 || body[0] != '{' {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		match := schedulerJobActionPath.FindStringSubmatch(r.URL.EscapedPath())
		if r.Method != "POST" || match == nil {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		var input map[string]json.RawMessage
		if json.Unmarshal(body, &input) != nil || len(input) != 0 {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Неизвестные параметры запроса."})
			return
		}
		e = save(ctx, match[1], match[2])
		if e != nil {
			if errors.Is(e, parser.ErrJobActionChanged) {
				jsonReply(w, 409, map[string]string{"error": "Задание выполняется, завершено или не найдено.", "code": "changed"})
			} else {
				schedulerSettingsFailure(w, e)
			}
			return
		}
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
}
