// Package workersupervisor содержит отключённую библиотечную композицию worker.
// Реального адаптера, команды запуска и production wiring здесь нет.
package workersupervisor

import (
	"context"
	"errors"
	"time"

	"itmap/internal/workerconfig"
	"itmap/internal/workerhealth"
	"itmap/internal/workerloop"
)

var ErrOpen = errors.New("worker runtime unavailable")
var ErrClose = errors.New("worker runtime close failed")

// Session — доверенный адаптер. Pipeline и Recover обязаны сами обеспечивать
// атомарный consumer fence, idempotency, paid guards и ограничение транзакций.
// Проверка БД или порядок вызовов этого интерфейса не обеспечивают эти свойства.
// Методы, включая Close, должны соблюдать контекст; внешних ошибок в лог нет.
type Session interface {
	Probe(context.Context) error
	Recover(context.Context) error
	Pipeline(context.Context) (bool, error)
	Close(context.Context) error
}

type Factory func(context.Context) (Session, error)

type Options struct {
	Getenv   func(string) string
	Open     Factory
	Health   *workerhealth.State
	Counters *workerloop.ProfileCounters
	// Profile задаёт интервалы только pipeline. Step и OnResult задаёт supervisor.
	Profile          workerloop.Profile
	RecoveryInterval time.Duration
	OpenTimeout      time.Duration
	CloseTimeout     time.Duration
}

// Run не запускается сам. До Factory проверяются оба enable-флага и вся
// конфигурация. Ошибки адаптера не возвращаются наружу в исходном виде.
// На выходе health помечается stopped, затем соединение закрывается. Ignoring
// контекст адаптером может задержать остановку: параллельный обход запрещён.
func Run(ctx context.Context, opt Options) (result error) {
	if ctx == nil || opt.Health == nil || opt.Counters == nil || opt.Open == nil {
		return workerloop.ErrConfig
	}
	if !opt.Health.StartOnce() {
		return workerloop.ErrConfig
	}
	defer opt.Health.Stop()
	if _, err := workerconfig.Parse(opt.Getenv); err != nil {
		return err
	}
	if opt.RecoveryInterval < time.Second || opt.RecoveryInterval > 24*time.Hour || opt.OpenTimeout < 10*time.Millisecond || opt.OpenTimeout > time.Minute || opt.CloseTimeout < 10*time.Millisecond || opt.CloseTimeout > time.Minute || opt.Profile.Name != workerloop.Pipeline || opt.Profile.Step != nil || opt.Profile.OnResult != nil {
		return workerloop.ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p := opt.Profile
	p.Step = func(context.Context) (bool, error) { return false, nil }
	if err := workerloop.ValidateProfiles([]workerloop.Profile{p}); err != nil {
		return err
	}
	openCtx, cancelOpen := context.WithTimeout(ctx, opt.OpenTimeout)
	session, err := opt.Open(openCtx)
	openExpired := openCtx.Err() != nil
	cancelOpen()
	if err != nil || openExpired {
		if session != nil {
			opt.Health.Stop()
			closeCtx, cancel := context.WithTimeout(context.Background(), opt.CloseTimeout)
			closeErr := session.Close(closeCtx)
			closeExpired := closeCtx.Err() != nil
			cancel()
			if closeErr != nil || closeExpired {
				return errors.Join(ErrOpen, ErrClose)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrOpen
	}
	if session == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrOpen
	}
	defer func() {
		opt.Health.Stop()
		closeCtx, cancel := context.WithTimeout(context.Background(), opt.CloseTimeout)
		defer cancel()
		if err := session.Close(closeCtx); err != nil || closeCtx.Err() != nil {
			result = ErrClose
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	var nextRecovery time.Time
	p.Step = func(stepCtx context.Context) (bool, error) {
		err := session.Probe(stepCtx)
		opt.Health.DatabaseCheck(err == nil && stepCtx.Err() == nil)
		if err != nil {
			return false, err
		}
		if err := stepCtx.Err(); err != nil {
			return false, err
		}
		if nextRecovery.IsZero() || !time.Now().Before(nextRecovery) {
			if err := session.Recover(stepCtx); err != nil {
				return false, err
			}
			if err := stepCtx.Err(); err != nil {
				return false, err
			}
			nextRecovery = time.Now().Add(opt.RecoveryInterval)
		}
		return session.Pipeline(stepCtx)
	}
	p.OnResult = func(name workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
		opt.Health.WorkResult(outcome == workerloop.ProfileWorked || outcome == workerloop.ProfileIdle, outcome == workerloop.ProfileTimeout)
		opt.Health.Progress()
		opt.Counters.Record(name, outcome)
	}
	return workerloop.RunProfiles(ctx, []workerloop.Profile{p}, nil)
}
