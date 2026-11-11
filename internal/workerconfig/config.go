// Package workerconfig проверяет ограничения до открытия БД и запуска worker.
package workerconfig

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var (
	ErrDisabled = errors.New("worker is not explicitly enabled")
	ErrDeadline = errors.New("worker pilot deadline is missing, invalid or expired")
)

// Config не содержит секретов и не запускает обработку.
type Config struct {
	Until            time.Time
	IdleDelay        time.Duration
	ErrorMin         time.Duration
	ErrorMax         time.Duration
	RecoveryInterval time.Duration
}

// Parse сохраняет действующую границу пилота из scheduler tick: Unix seconds,
// только ASCII-цифры, не более 12 символов. Разрешение worker задаётся отдельно.
func Parse(getenv func(string) string, now time.Time) (Config, error) {
	if getenv == nil || getenv("ITMAP_WORKER_ENABLED") != "1" || getenv("ITMAP_PARSER_EXECUTION_ENABLED") != "1" {
		return Config{}, ErrDisabled
	}
	raw := getenv("ITMAP_SCHEDULER_UNTIL")
	if len(raw) == 0 || len(raw) > 12 {
		return Config{}, ErrDeadline
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return Config{}, ErrDeadline
		}
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return Config{}, ErrDeadline
	}
	until := time.Unix(seconds, 0)
	if !now.Before(until) {
		return Config{}, ErrDeadline
	}
	return Config{Until: until, IdleDelay: 5 * time.Second, ErrorMin: 5 * time.Second, ErrorMax: time.Minute, RecoveryInterval: time.Minute}, nil
}

// Context ограничивает всю работу подтверждённым сроком пилота и родителем.
func (c Config) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, c.Until)
}
