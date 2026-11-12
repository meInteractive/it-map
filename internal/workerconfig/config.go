// Package workerconfig проверяет ограничения до открытия БД и запуска worker.
package workerconfig

import (
	"context"
	"errors"
	"time"
)

var ErrDisabled = errors.New("worker is not explicitly enabled")

// Config не содержит секретов и не запускает обработку.
type Config struct {
	IdleDelay        time.Duration
	ErrorMin         time.Duration
	ErrorMax         time.Duration
	RecoveryInterval time.Duration
}

// Parse проверяет явное разрешение до открытия базы и запуска обработки.
func Parse(getenv func(string) string) (Config, error) {
	if getenv == nil || getenv("ITMAP_WORKER_ENABLED") != "1" || getenv("ITMAP_PARSER_EXECUTION_ENABLED") != "1" {
		return Config{}, ErrDisabled
	}
	return Config{IdleDelay: 5 * time.Second, ErrorMin: 5 * time.Second, ErrorMax: time.Minute, RecoveryInterval: time.Minute}, nil
}

// Context обеспечивает отмену работы вместе с родительским контекстом.
func (c Config) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}
