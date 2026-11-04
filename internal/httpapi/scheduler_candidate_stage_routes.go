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

type SchedulerCandidateStageRunner interface {
	PrepareCandidateStage(context.Context) (func(context.Context, string, parser.ScanCandidateScope) (json.RawMessage, error), error)
}

// Продолжение доступно только администратору и подтверждённому сканированию.
func SchedulerCandidateStageHandler(manager *auth.Manager, runner SchedulerCandidateStageRunner) http.HandlerFunc {
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
		run, err := runner.PrepareCandidateStage(capture)
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
		decoder := json.NewDecoder(bytes.NewReader(raw))
		token, err := decoder.Token()
		if err != nil || token != json.Delim('{') {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и идентификатор компании."})
			return
		}
		fields := make(map[string]json.RawMessage)
		for decoder.More() {
			key, keyErr := decoder.Token()
			name, ok := key.(string)
			if keyErr != nil || !ok || fields[name] != nil {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и идентификатор компании."})
				return
			}
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и идентификатор компании."})
				return
			}
			fields[name] = value
		}
		end, endErr := decoder.Token()
		var trailing any
		if endErr != nil || end != json.Delim('}') || decoder.Decode(&trailing) != io.EOF {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и идентификатор компании."})
			return
		}
		var requestID, scanRequestID, scanJobID string
		var companyID int64
		if len(fields) != 4 || json.Unmarshal(fields["scanRequestId"], &scanRequestID) != nil || json.Unmarshal(fields["scanJobId"], &scanJobID) != nil || !schedulerCycleUUID.MatchString(scanRequestID) || !schedulerCycleUUID.MatchString(scanJobID) || json.Unmarshal(fields["requestId"], &requestID) != nil || json.Unmarshal(fields["companyId"], &companyID) != nil || !schedulerCycleUUID.MatchString(requestID) || companyID < 1 || companyID > 9007199254740991 {
			schedulerSettingsFailure(w, &parser.SettingsError{Message: "Передайте UUID запуска и идентификатор компании."})
			return
		}
		result, err := run(r.Context(), requestID, parser.ScanCandidateScope{RequestID: scanRequestID, ScanJobID: scanJobID, CompanyID: companyID})
		if errors.Is(err, parser.ErrRunBusy) {
			jsonReply(w, 409, map[string]string{"error": "У компании уже есть активное задание или выполняется другой цикл.", "code": "scheduler_busy"})
			return
		}
		if errors.Is(err, parser.ErrHHListingChanged) {
			jsonReply(w, 409, map[string]string{"error": "Сканирование недоступно для продолжения.", "code": "scope_unavailable"})
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
