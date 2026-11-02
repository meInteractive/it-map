package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"itmap/internal/parser"
)

type SchedulerTickRunner interface {
	PrepareTick(context.Context) (func(context.Context, string, bool) (json.RawMessage, error), error)
}

func SchedulerTickHandler(runner SchedulerTickRunner, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if runner == nil || token == "" {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		submitted := r.Header.Get("Authorization")
		if !strings.HasPrefix(submitted, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(submitted, "Bearer ")), []byte(token)) != 1 {
			jsonReply(w, 401, map[string]string{"error": "Недействительный ключ диспетчера."})
			return
		}
		if r.Method != "POST" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		run, err := runner.PrepareTick(ctx)
		cancel()
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		if run == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		if r.ContentLength > 32768 {
			schedulerInputTooLarge(w)
			return
		}
		if r.Body == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		if err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				schedulerInputTooLarge(w)
			} else {
				schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			}
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		for key := range fields {
			if key != "requestId" && key != "diagnostic" {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Неизвестные параметры запроса."})
				return
			}
		}
		id := ""
		if v, ok := fields["requestId"]; ok {
			if string(v) == "null" || json.Unmarshal(v, &id) != nil || len(utf16.Encode([]rune(id))) > 100 {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Некорректный сигнал."})
				return
			}
		}
		diagnostic := false
		if v, ok := fields["diagnostic"]; ok {
			if string(v) == "null" || json.Unmarshal(v, &diagnostic) != nil {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Некорректный сигнал."})
				return
			}
		}
		result, err := run(r.Context(), id, diagnostic)
		if errors.Is(err, parser.ErrRunBusy) {
			jsonReply(w, 409, map[string]string{"error": "Другой цикл диспетчера ещё выполняется.", "code": "scheduler_busy"})
			return
		}
		if errors.Is(err, parser.ErrRunGeneration) {
			schedulerSettingsFailure(w, parser.ErrSettingsGeneration)
			return
		}
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		jsonReply(w, 200, result)
	}
}
