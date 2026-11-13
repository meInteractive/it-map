// Package workerengine связывает непрерывный обход профилей с выполнением заданий.
package workerengine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"itmap/internal/parser"
	"itmap/internal/workerloop"
)

var ErrCycle = errors.New("worker cycle failed")

// Engine выполняет только один ограниченный цикл за раз. Владелец обязан
// обеспечить допустимость исполнителя и хранилища до запуска Run.
type Engine struct {
	Worker   parser.Worker
	OnResult func(workerloop.ProfileName, workerloop.ProfileOutcome)
}

// Run продолжает обход до отмены контекста. Новый случайный идентификатор
// каждого цикла исключает повторное использование результатов прежнего run.
// Ошибки шагов передаются только как фиксированная категория результата.
func (e Engine) Run(ctx context.Context) error {
	if ctx == nil || e.Worker.Runs.DB == nil || e.Worker.Settings.DB != e.Worker.Runs.DB {
		return workerloop.ErrConfig
	}
	makeProfile := func(name workerloop.ProfileName, idle time.Duration) workerloop.Profile {
		return workerloop.Profile{
			Name: name, WorkDelay: time.Second, IdleDelay: idle,
			ErrorMin: 5 * time.Second, ErrorMax: time.Minute, StepTimeout: 2 * time.Minute,
			OnResult: e.OnResult,
			Step:     func(ctx context.Context) (bool, error) { return e.cycle(ctx, name) },
		}
	}
	return workerloop.RunProfiles(ctx, []workerloop.Profile{
		makeProfile(workerloop.Launch, 30*time.Second),
		makeProfile(workerloop.Reconcile, 30*time.Second),
		makeProfile(workerloop.Pipeline, 5*time.Second),
	}, nil)
}

func (e Engine) cycle(ctx context.Context, profile workerloop.ProfileName) (bool, error) {
	// Launch также планирует источники и восстанавливает leases, поэтому его
	// нельзя пропускать только из-за отсутствия готового задания. Остальные
	// профили избегают пустого run; снимок не заменяет атомарный claim.
	if profile != workerloop.Launch {
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
	worker := e.Worker
	worker.ScheduleSources = profile == workerloop.Launch
	worker.VerifiedHHOnly = true
	worker.ContinueOnStageFailure = true
	worker.DeferUnspentBudget = true
	result, err := worker.Cycle(ctx, parser.WorkerOptions{
		Run:     parser.RunOptions{RequestID: id, Trigger: "automatic", Duration: 100 * time.Second},
		MaxJobs: 50, QueueProfile: string(profile),
	})
	if err != nil || result.Reused || result.Run.Status != "completed" {
		return false, ErrCycle
	}
	return result.Processed > 0, nil
}
