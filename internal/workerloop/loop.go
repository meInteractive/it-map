// Package workerloop содержит отключённый по умолчанию механизм ожидания работы.
package workerloop

import (
	"context"
	"errors"
	"math/rand"
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
	Wakeup    <-chan struct{}
	// WakeupMinInterval ограничивает частоту сигналов, не блокируя polling.
	WakeupMinInterval time.Duration
}

var ErrConfig = errors.New("invalid worker loop configuration")

// jitter сохраняет не менее 75% задержки и никогда не превышает её.
func jitter(delay time.Duration) time.Duration {
	spread := delay / 4
	if spread == 0 {
		return delay
	}
	return delay - time.Duration(rand.Int63n(int64(spread)+1))
}

// Run вызывает step последовательно до отмены контекста. Ошибки повторяются
// с ограниченной задержкой; отмена прерывает как step, так и ожидание.
func Run(ctx context.Context, cfg Config, step Step) error {
	if ctx == nil || step == nil || cfg.IdleDelay <= 0 || cfg.ErrorMin <= 0 || cfg.ErrorMax < cfg.ErrorMin || (cfg.Wakeup != nil && cfg.WakeupMinInterval <= 0) {
		return ErrConfig
	}
	delay := cfg.ErrorMin
	wakeup := cfg.Wakeup
	var nextWake time.Time
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
			timer := time.NewTimer(jitter(wait))
			// Сигнал ускоряет только idle polling, не обходя backoff ошибок.
			signal := wakeup
			if err != nil {
				signal = nil
			}
			var gate *time.Timer
			var gateC <-chan time.Time
			if signal != nil && time.Now().Before(nextWake) {
				signal = nil
				gate = time.NewTimer(time.Until(nextWake))
				gateC = gate.C
			}
		waiting:
			for {
				select {
				case <-ctx.Done():
					timer.Stop()
					if gate != nil {
						gate.Stop()
					}
					return ctx.Err()
				case <-gateC:
					gateC = nil
					signal = wakeup
				case _, open := <-signal:
					if !open {
						wakeup, signal = nil, nil
						continue
					}
					timer.Stop()
					if gate != nil {
						gate.Stop()
					}
					nextWake = time.Now().Add(cfg.WakeupMinInterval)
					break waiting
				case <-timer.C:
					if gate != nil {
						gate.Stop()
					}
					break waiting
				}
			}
		}
	}
}
