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

type SchedulerResumeRunner interface {
	PrepareResume(context.Context) (func(context.Context, string, string) (json.RawMessage, error), error)
}

// Возобновление доступно только администратору и одному выбранному заданию.
func SchedulerResumeHandler(manager *auth.Manager, runner SchedulerResumeRunner) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin || !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору сайта."})
			return
		}
		if runner == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище не подключено."})
			return
		}
		capture, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		run, err := runner.PrepareResume(capture)
		cancel()
		if err != nil || run == nil {
			if err == nil {
				err = parser.ErrSettingsStorage
			}
			schedulerSettingsFailure(w, err)
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
		var requestID, jobID string
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || json.Unmarshal(fields["requestId"], &requestID) != nil || json.Unmarshal(fields["jobId"], &jobID) != nil || !schedulerCycleUUID.MatchString(requestID) || !schedulerCycleUUID.MatchString(jobID) {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и задания."})
			return
		}
		result, err := run(r.Context(), requestID, jobID)
		if errors.Is(err, parser.ErrRunBusy) {
			jsonReply(w, 409, map[string]string{"error": "Другой цикл диспетчера ещё выполняется.", "code": "scheduler_busy"})
			return
		}
		if errors.Is(err, parser.ErrHHListingChanged) {
			jsonReply(w, 409, map[string]string{"error": "Для этого задания нет доступного сохранённого списка.", "code": "resume_unavailable"})
			return
		}
		if errors.Is(err, parser.ErrRunGeneration) {
			err = parser.ErrSettingsGeneration
		}
		if err != nil {
			schedulerSettingsFailure(w, err)
			return
		}
		jsonReply(w, 200, result)
	})
}
