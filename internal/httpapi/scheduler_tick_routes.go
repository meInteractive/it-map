package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"itmap/internal/parser"
)

type SchedulerTickRunner interface {
	PrepareTick(context.Context) (func(context.Context, string, bool) (json.RawMessage, error), error)
}

// Не расширяем обязательный контракт старого тика: профиль поддерживается
// только новыми Go-реализациями и имеет тот же bearer-token.
type SchedulerProfileTickRunner interface {
	PrepareProfileTick(context.Context) (func(context.Context, string, string) (json.RawMessage, error), error)
}

func SchedulerTickHandler(runner SchedulerTickRunner, token string, samplers ...SchedulerIssueSampler) http.HandlerFunc {
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
		var profileRun func(context.Context, string, string) (json.RawMessage, error)
		if err == nil {
			if profiled, ok := runner.(SchedulerProfileTickRunner); ok {
				profileRun, err = profiled.PrepareProfileTick(ctx)
			}
		}
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
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			schedulerSettingsFailure(w, parser.ErrSettingsStorage)
			return
		}
		for key := range fields {
			if key != "requestId" && key != "diagnostic" && key != "profile" {
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
		profile := ""
		if v, ok := fields["profile"]; ok {
			if json.Unmarshal(v, &profile) != nil || profile == "" || profile != "launch" && profile != "reconcile" && profile != "pipeline" || diagnostic || id == "" {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Некорректный профиль очереди."})
				return
			}
			if profileRun == nil {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Профиль очереди не поддерживается."})
				return
			}
		}
		// История записывается при штатном сигнале, даже если админ-панель закрыта.
		// Отказ аналитики не должен прерывать оплаченный конвейер или менять его ответ.
		if !diagnostic && len(samplers) > 0 && samplers[0] != nil {
			defer func() {
				sampleCtx, sampleCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
				defer sampleCancel()
				if err := samplers[0].Sample(sampleCtx); err != nil {
					slog.Warn("scheduler issues sample failed", "component", "parser_error_daily")
				}
			}()
		}
		var result json.RawMessage
		if profile != "" {
			result, err = profileRun(r.Context(), id, profile)
		} else {
			result, err = run(r.Context(), id, diagnostic)
		}
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
