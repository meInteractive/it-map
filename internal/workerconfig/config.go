// Package workerconfig проверяет ограничения до открытия БД и запуска worker.
package workerconfig

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var ErrDisabled = errors.New("worker is not explicitly enabled")
var ErrConfig = errors.New("invalid worker runtime configuration")

// Config не содержит секретов и не запускает обработку.
type Config struct {
	IdleDelay        time.Duration
	ErrorMin         time.Duration
	ErrorMax         time.Duration
	RecoveryInterval time.Duration
	LaunchIdle       time.Duration
	ReconcileIdle    time.Duration
	MaxJobs          int
}

// Defaults сохраняет прежние интервалы и лимит работы за цикл.
func Defaults() Config {
	return Config{IdleDelay: 5 * time.Second, ErrorMin: 5 * time.Second, ErrorMax: time.Minute, RecoveryInterval: time.Minute, LaunchIdle: 30 * time.Second, ReconcileIdle: 30 * time.Second, MaxJobs: 50}
}

// Validate ограничивает polling и batch size; бюджеты и частота Actor задаются отдельно.
func (c Config) Validate() error {
	if c.IdleDelay < time.Second || c.IdleDelay > 60*time.Second || c.LaunchIdle < 15*time.Second || c.LaunchIdle > 60*time.Second || c.ReconcileIdle < 5*time.Second || c.ReconcileIdle > 60*time.Second || c.ErrorMin < time.Second || c.ErrorMax < c.ErrorMin || c.ErrorMax > 2*time.Minute || c.RecoveryInterval <= 0 || c.MaxJobs < 1 || c.MaxJobs > 50 {
		return ErrConfig
	}
	return nil
}

func positiveInteger(raw string) (int, error) {
	if raw == "" {
		return 0, ErrConfig
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, ErrConfig
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, ErrConfig
	}
	return n, nil
}

// Parse проверяет явное разрешение до открытия базы и запуска обработки.
func Parse(getenv func(string) string) (Config, error) {
	if getenv == nil || getenv("ITMAP_WORKER_ENABLED") != "1" || getenv("ITMAP_PARSER_EXECUTION_ENABLED") != "1" {
		return Config{}, ErrDisabled
	}
	c := Defaults()
	for _, v := range []struct {
		key    string
		target *time.Duration
	}{
		{"ITMAP_WORKER_LAUNCH_IDLE_SECONDS", &c.LaunchIdle},
		{"ITMAP_WORKER_RECONCILE_IDLE_SECONDS", &c.ReconcileIdle},
		{"ITMAP_WORKER_PIPELINE_IDLE_SECONDS", &c.IdleDelay},
		{"ITMAP_WORKER_ERROR_MIN_SECONDS", &c.ErrorMin},
		{"ITMAP_WORKER_ERROR_MAX_SECONDS", &c.ErrorMax},
	} {
		if raw := getenv(v.key); raw != "" {
			n, err := positiveInteger(raw)
			if err != nil || n > 120 {
				return Config{}, ErrConfig
			}
			*v.target = time.Duration(n) * time.Second
		}
	}
	if raw := getenv("ITMAP_WORKER_MAX_JOBS"); raw != "" {
		n, err := positiveInteger(raw)
		if err != nil {
			return Config{}, ErrConfig
		}
		c.MaxJobs = n
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Context обеспечивает отмену работы вместе с родительским контекстом.
func (c Config) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}
