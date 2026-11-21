// Package workerengine связывает непрерывный обход профилей с выполнением заданий.
package workerengine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"itmap/internal/parser"
	"itmap/internal/workerconfig"
	"itmap/internal/workerloop"
)

const profileStepTimeout = 2 * time.Minute

// MonitorMaxAge accounts for serialized steps and bounded scheduling delay.
// A delayed heartbeat still does not prove the worker or providers are healthy.
func MonitorMaxAge(config workerconfig.Config) time.Duration {
	delay := config.LaunchIdle
	for _, candidate := range []time.Duration{config.ReconcileIdle, config.IdleDelay} {
		if candidate > delay {
			delay = candidate
		}
	}
	delay += delay / 4 // profile jitter never exceeds 25 percent
	if config.ErrorMax > delay {
		delay = config.ErrorMax
	}
	return 3*profileStepTimeout + delay + time.Second
}

var ErrCycle = errors.New("worker cycle failed")
var ErrGenerationChanged = errors.New("worker generation changed; restart required")

// Engine выполняет только один ограниченный цикл за раз. Владелец обязан
// обеспечить допустимость исполнителя и хранилища до запуска Run.
type Engine struct {
	Worker   parser.Worker
	OnResult func(workerloop.ProfileName, workerloop.ProfileOutcome)
	Config   *workerconfig.Config
	Pause    *PauseGate
}

// Run продолжает обход до отмены контекста. Новый случайный идентификатор
// каждого цикла исключает повторное использование результатов прежнего run.
// Ошибки шагов передаются только как фиксированная категория результата.
func (e Engine) Run(ctx context.Context) error {
	if ctx == nil || e.Worker.Runs.DB == nil || e.Worker.Settings.DB != e.Worker.Runs.DB {
		return workerloop.ErrConfig
	}
	config := workerconfig.Defaults()
	if e.Config != nil {
		config = *e.Config
	}
	if err := config.Validate(); err != nil {
		return workerloop.ErrConfig
	}
	err := workerloop.RunProfilesWithGrace(ctx, e.profiles(ctx, config), nil, config.ShutdownGrace)
	if errors.Is(err, workerloop.ErrHalt) {
		return ErrGenerationChanged
	}
	return err
}

func (e Engine) profiles(stop context.Context, config workerconfig.Config) []workerloop.Profile {
	makeProfile := func(name workerloop.ProfileName, idle time.Duration) workerloop.Profile {
		return workerloop.Profile{
			Name: name, WorkDelay: time.Second, IdleDelay: idle,
			ErrorMin: config.ErrorMin, ErrorMax: config.ErrorMax, StepTimeout: profileStepTimeout,
			OnResult: e.OnResult,
			Step: func(ctx context.Context) (bool, error) {
				return e.runProfile(ctx, stop, name, config.MaxJobs)
			},
		}
	}
	return []workerloop.Profile{
		makeProfile(workerloop.Launch, config.LaunchIdle),
		makeProfile(workerloop.Reconcile, config.ReconcileIdle),
		makeProfile(workerloop.Pipeline, config.IdleDelay),
	}
}

func (e Engine) runProfile(ctx, stop context.Context, name workerloop.ProfileName, maxJobs int) (worked bool, err error) {
	if e.Pause == nil {
		return e.cycle(ctx, stop.Done(), name, maxJobs)
	}
	claims, err := e.Pause.enter(ctx)
	if err != nil {
		return false, err
	}
	// Общий сигнал сохраняет прежнее прекращение claims при SIGTERM.
	cancel := context.AfterFunc(stop, claims.stop)
	defer cancel()
	if stop.Err() != nil {
		claims.stop()
	}
	safe := false
	defer func() { e.Pause.leave(safe) }()
	worked, err = e.cycle(ctx, claims.done, name, maxJobs)
	safe = err == nil && ctx.Err() == nil
	return worked, err
}

func (e Engine) cycle(ctx context.Context, stopClaims <-chan struct{}, profile workerloop.ProfileName, maxJobs int) (bool, error) {
	// После restore продолжать работу со старым generation бессмысленно;
	// при доступной БД завершаем процесс, чтобы supervisor перечитал состояние.
	generation, err := e.Worker.Settings.Generation(ctx)
	if err != nil {
		return false, ErrCycle
	}
	if generation != e.Worker.Runs.Generation {
		return false, workerloop.ErrHalt
	}
	// Launch также планирует источники и восстанавливает leases, поэтому его
	// нельзя пропускать только из-за отсутствия готового задания. Остальные
	// профили избегают пустого run; снимок не заменяет атомарный claim.
	if profile == workerloop.Launch {
		enabled, err := e.Worker.Runs.SchedulerEnabledSnapshot(ctx)
		if err != nil || !enabled {
			return false, ErrCycle
		}
	} else {
		source := "hh"
		options := parser.ClaimOptions{SourceID: &source, VerifiedHHOnly: true}
		switch profile {
		case workerloop.Reconcile:
			options.Types = []string{"scan_source"}
			options.HHScanPhase = "reconcile"
		case workerloop.Pipeline:
			options.Types = []string{"classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}
		default:
			return false, ErrCycle
		}
		ready, err := e.Worker.Runs.HasEligibleJob(ctx, options)
		if err != nil {
			return false, ErrCycle
		}
		if !ready {
			return false, nil
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return false, ErrCycle
	}
	id := "worker:" + hex.EncodeToString(raw[:])
	worker := e.workerForProfile(profile)
	// Резервируем минуту перед любым новым pipeline job, включая первый.
	// Долгие AI-вызовы всё ещё могут превысить дедлайн.
	var minRemaining time.Duration
	if profile == workerloop.Pipeline {
		minRemaining = 60 * time.Second
	}
	result, err := worker.Cycle(ctx, parser.WorkerOptions{
		Run:     parser.RunOptions{RequestID: id, Trigger: "automatic", Duration: 100 * time.Second},
		MaxJobs: maxJobs, QueueProfile: string(profile), StopClaims: stopClaims, MinRemaining: minRemaining,
	})
	if err == parser.ErrRunBusy {
		return false, workerloop.ErrRunContended
	}
	if err != nil || result.Reused || result.Run.Status != "completed" {
		return false, ErrCycle
	}
	return result.Processed > 0, nil
}

// workerForProfile uses the same durable paid-start cooldown in every queue.
func (e Engine) workerForProfile(profile workerloop.ProfileName) parser.Worker {
	worker := e.Worker
	worker.ScheduleSources = profile == workerloop.Launch
	worker.VerifiedHHOnly = true
	worker.ContinueOnStageFailure = true
	worker.DeferUnspentBudget = true
	worker.EnforceActorCooldown = true
	return worker
}
