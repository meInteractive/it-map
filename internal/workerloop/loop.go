// Package workerloop содержит отключённый по умолчанию механизм ожидания работы.
package workerloop

import (
	"context"
	"errors"
	"time"
)

// Step выполняет ограниченную порцию работы и сообщает, была ли она найдена.
type Step func(context.Context) (bool, error)

// Config ограничивает частоту пустых и неуспешных попыток.
type Config struct {
	IdleDelay time.Duration
	ErrorMin  time.Duration
	ErrorMax  time.Duration
	OnError   func(error)
}

var ErrConfig = errors.New("invalid worker loop configuration")

// Run вызывает step последовательно до отмены контекста. Ошибки повторяются
// с ограниченной задержкой; отмена прерывает как step, так и ожидание.
func Run(ctx context.Context, cfg Config, step Step) error {
	if ctx == nil || step == nil || cfg.IdleDelay <= 0 || cfg.ErrorMin <= 0 || cfg.ErrorMax < cfg.ErrorMin {
		return ErrConfig
	}
	delay := cfg.ErrorMin
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := step(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := time.Duration(0)
		if err != nil {
			if cfg.OnError != nil {
				cfg.OnError(err)
			}
			wait = delay
			if delay > cfg.ErrorMax/2 {
				delay = cfg.ErrorMax
			} else {
				delay *= 2
			}
		} else {
			delay = cfg.ErrorMin
			if !worked {
				wait = cfg.IdleDelay
			}
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
