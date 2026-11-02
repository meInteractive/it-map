package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerCycleRunner interface {
	PrepareCycle(context.Context) (func(context.Context, string) (json.RawMessage, error), error)
}

var schedulerCycleUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func SchedulerCycleHandler(manager *auth.Manager, runner SchedulerCycleRunner) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if runner == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		capture, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		run, err := runner.PrepareCycle(capture)
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
		raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
		var input map[string]json.RawMessage
		if json.Unmarshal(raw, &input) != nil || input == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		var id string
		if len(input) != 1 || json.Unmarshal(input["requestId"], &id) != nil || !schedulerCycleUUID.MatchString(id) {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска."})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "application/x-ndjson") {
			var read func(context.Context, json.RawMessage) (json.RawMessage, error)
			if reader, ok := runner.(SchedulerRunReader); ok {
				snapshot, cancel := context.WithTimeout(r.Context(), 2*time.Second)
				read, _ = reader.PrepareRunRead(snapshot)
				cancel()
			}
			streamSchedulerCycle(w, r, id, run, read)
			return
		}
		result, err := run(r.Context(), id)
		if errors.Is(err, parser.ErrRunGeneration) {
			schedulerSettingsFailure(w, parser.ErrSettingsGeneration)
			return
		}
		if errors.Is(err, parser.ErrRunBusy) {
			jsonReply(w, 409, map[string]string{"error": "Другой цикл диспетчера ещё выполняется.", "code": "scheduler_busy"})
			return
		}
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		jsonReply(w, 200, result)
	})
}
