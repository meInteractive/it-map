package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"itmap/internal/auth"
	"itmap/internal/parser"
	"net/http"
	"time"
)

type SchedulerProbeRunner interface {
	PrepareProbe(context.Context) (func(context.Context, string) (json.RawMessage, error), error)
}

func SchedulerProbeHandler(manager *auth.Manager, runner SchedulerProbeRunner) http.HandlerFunc {
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
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		run, err := runner.PrepareProbe(ctx)
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
		if len(fields) != 0 {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Неизвестные параметры запроса."})
			return
		}
		result, err := run(r.Context(), "")
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
	})
}
