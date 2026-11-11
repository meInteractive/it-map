package workerconfig

import (
	"context"
	"errors"

	"itmap/internal/parser"
)

// ErrorCode возвращает только фиксированные коды, не текст ошибок БД/провайдеров.
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, parser.ErrRunBusy):
		return "scheduler_busy"
	case errors.Is(err, parser.ErrRunGeneration):
		return "generation_changed"
	case errors.Is(err, parser.ErrRunLeaseLost), errors.Is(err, parser.ErrJobLeaseLost):
		return "lease_lost"
	case errors.Is(err, parser.ErrPipelinePartial):
		return "pipeline_partial"
	case errors.Is(err, parser.ErrRunStorage), errors.Is(err, parser.ErrSettingsStorage):
		return "storage_unavailable"
	default:
		return "worker_error"
	}
}
