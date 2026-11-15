package apifyruns

import (
	"errors"
	"itmap/internal/integrations/apify"
)

// ErrReadTemporary разрешает повтор только известного GET, не POST Actor.
var ErrReadTemporary = errors.New("apify read temporarily unavailable")

// Классифицируем только фиксированные ошибки адаптера. Произвольные и
// составные ошибки не превращаем в разрешение повторять операцию.
func classifyReadError(err error) error {
	e, ok := err.(*apify.Error)
	if !ok || e == nil {
		return err
	}
	switch e.Code {
	case "apify_network", "apify_timeout", "apify_busy":
		return ErrReadTemporary
	default:
		return err
	}
}
