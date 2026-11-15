package apifyruns

import (
	"errors"
	"itmap/internal/integrations/apify"
	"itmap/internal/retryafter"
	"time"
)

// ErrReadTemporary разрешает повтор только известного GET, не POST Actor.
var ErrReadTemporary = errors.New("apify read temporarily unavailable")

// ErrReadBudget относится только к GET; он не разрешает запуск другого Actor.
var ErrReadBudget = errors.New("apify read blocked by provider budget")

// ReadTemporaryError carries a bounded GET retry hint without provider data.
type ReadTemporaryError struct{ RetryAfter time.Duration }

func (e *ReadTemporaryError) Error() string { return ErrReadTemporary.Error() }
func (e *ReadTemporaryError) Unwrap() error { return ErrReadTemporary }

// Классифицируем только фиксированные ошибки адаптера. Произвольные и
// составные ошибки не превращаем в разрешение повторять операцию.
func classifyReadError(err error) error {
	e, ok := err.(*apify.Error)
	if !ok || e == nil {
		return err
	}
	if e.RetryableRead && e.Code == "apify_data" && e.Status == 503 {
		if e.RetryAfter >= retryafter.MinDelay && e.RetryAfter <= retryafter.MaxDelay {
			return &ReadTemporaryError{RetryAfter: e.RetryAfter}
		}
		return ErrReadTemporary
	}
	switch e.Code {
	case "apify_budget":
		return ErrReadBudget
	case "apify_network", "apify_timeout", "apify_busy":
		if e.RetryAfter >= retryafter.MinDelay && e.RetryAfter <= retryafter.MaxDelay {
			return &ReadTemporaryError{RetryAfter: e.RetryAfter}
		}
		return ErrReadTemporary
	default:
		return err
	}
}
