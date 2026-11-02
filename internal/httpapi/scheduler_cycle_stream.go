package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"itmap/internal/parser"
)

type cycleStreamResult struct {
	body json.RawMessage
	err  error
}

func streamSchedulerCycle(w http.ResponseWriter, r *http.Request, id string, run func(context.Context, string) (json.RawMessage, error), read func(context.Context, json.RawMessage) (json.RawMessage, error)) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	send := func(v any) bool {
		if encoder.Encode(v) != nil {
			return false
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return true
	}
	start := time.Now()
	processed, failures := 0, 0
	progress := func() bool {
		return send(map[string]any{"type": "progress", "processed": processed, "errors": failures, "elapsedSeconds": int(time.Since(start).Seconds())})
	}
	if !progress() {
		return
	}
	done := make(chan cycleStreamResult, 1)
	go func() { body, err := run(ctx, id); done <- cycleStreamResult{body, err} }()
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	query, _ := json.Marshal(map[string]string{"requestId": id})
	for {
		select {
		case result := <-done:
			if result.err != nil {
				status, code, message := 500, "storage", "Не удалось завершить цикл. Сохранённые этапы доступны в очереди."
				if errors.Is(result.err, parser.ErrRunBusy) {
					status, code, message = 409, "scheduler_busy", "Другой цикл диспетчера ещё выполняется."
				} else if errors.Is(result.err, parser.ErrRunGeneration) || errors.Is(result.err, parser.ErrSettingsGeneration) {
					status, code, message = 409, "restored", "База восстановлена во время операции."
				}
				send(map[string]any{"type": "error", "error": message, "status": status, "code": code})
				return
			}
			send(map[string]any{"type": "done", "run": result.body})
			return
		case <-ticker.C:
			if read != nil {
				poll, cancelPoll := context.WithTimeout(ctx, 2*time.Second)
				raw, err := read(poll, query)
				cancelPoll()
				if err == nil {
					var snapshot struct {
						Processed int `json:"processed"`
						Errors    int `json:"errors"`
					}
					if json.Unmarshal(raw, &snapshot) == nil {
						processed, failures = snapshot.Processed, snapshot.Errors
					}
				}
			}
			if !progress() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
