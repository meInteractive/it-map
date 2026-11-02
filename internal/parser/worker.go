package parser

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
)

// Worker запускается только явным вызовом Cycle; таймеров и фоновых задач нет.
type Worker struct {
	Runs           RunRepository
	Settings       apifysettings.Repository
	ListingRuntime HHListingRuntime
	DetailRuntime  HHDetailRuntime
	Public         HHPublicFetcher
	Generator      AIStageGenerator
	Classifier     AIStageClassifier
}

// NewWorker связывает провайдеры только с серверным хранилищем конфигурации.
func NewWorker(runs RunRepository, settings apifysettings.Repository, providers aiproviders.Repository) (Worker, error) {
	if runs.DB == nil || settings.DB != runs.DB || providers.DB != runs.DB {
		return Worker{}, ErrRunInput
	}
	router := aiproviders.NewRouterService(providers)
	openAI := aiproviders.NewOpenAIService(providers)
	return Worker{Runs: runs, Settings: settings, Generator: aiproviders.NewGenerator(openAI, router), Classifier: router}, nil
}

type WorkerOptions struct {
	Run     RunOptions
	MaxJobs int
}
type WorkerResult struct {
	Run       Run
	Processed int
	Reused    bool
}

var workerTypes = []string{"scan_source", "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}

func (w Worker) Cycle(ctx context.Context, options WorkerOptions) (result WorkerResult, failure error) {
	if w.Runs.DB == nil || w.Settings.DB != w.Runs.DB || options.MaxJobs < 1 || options.MaxJobs > 1000 || options.Run.Diagnostic {
		return result, ErrRunInput
	}
	duration := options.Run.Duration
	if duration == 0 {
		duration = 110 * time.Second
	}
	if duration < time.Millisecond || duration > 5*time.Minute {
		return result, ErrRunInput
	}
	start, err := w.Runs.Begin(ctx, options.Run)
	if err != nil {
		return result, err
	}
	result.Run, result.Reused = start.Run, start.Reused
	if !start.Acquired {
		return result, nil
	}
	status := "completed"
	defer func() {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := w.Runs.Finish(finishCtx, start.Run.ID, status); err != nil {
			failure = errors.Join(failure, err)
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	raw, err := (SettingsRepository{DB: w.Runs.DB}).Read(bounded)
	if err != nil {
		status = "partial"
		return result, err
	}
	var settings Settings
	if json.Unmarshal(raw, &settings) != nil || !settings.Enabled || settings.OwnerName == "" {
		status = "partial"
		return result, ErrSettingsStorage
	}
	owner := settings.OwnerName
	listingRuntime := w.ListingRuntime
	if listingRuntime == nil {
		listingRuntime = apifyruns.Service{Repository: apifyruns.Repository{DB: w.Settings.DB, Settings: w.Settings}}
	}
	d := workerDispatch{
		listing:  HHListingExecutor{Settings: w.Settings, Runtime: listingRuntime, Owner: owner},
		detail:   HHFullTextExecutor{Settings: w.Settings, Runtime: w.DetailRuntime, Public: w.Public, Owner: owner},
		importer: HHImportExecutor{Settings: w.Settings, Owner: owner},
		ai:       AIStageDispatcher{Owner: owner, Generator: w.Generator, Classifier: w.Classifier},
	}
	for result.Processed < options.MaxJobs {
		if err = bounded.Err(); err != nil {
			status = "interrupted"
			return result, err
		}
		source := "hh"
		lease, err := w.Runs.ClaimJob(bounded, start.Run.ID, ClaimOptions{Types: workerTypes, SourceID: &source})
		if err != nil {
			status = "partial"
			return result, err
		}
		if lease == nil {
			return result, nil
		}
		result.Processed++
		if err = workerExecute(bounded, d, lease); err != nil {
			status = "partial"
			return result, err
		}
	}
	return result, nil
}

func workerExecute(ctx context.Context, d workerDispatch, lease *JobLease) error {
	kind := lease.Job().Type
	ai := kind == "classify_title" || kind == "extract_vacancy" || kind == "assign_roles" || kind == "score_relevance"
	var attempt *Attempt
	var err error
	if !ai {
		attempt, err = lease.StartAttempt(ctx)
		if err != nil {
			return err
		}
	}
	out, executeErr := d.execute(ctx, lease)
	if ai && out.finished {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	finish := FinishOptions{Status: "completed"}
	if executeErr != nil {
		finish = FinishOptions{Status: "failed", Code: "stage_failed", Message: "Не удалось выполнить этап обработки."}
		if errors.Is(executeErr, ErrHHDetailPending) || errors.Is(executeErr, ErrDetailBusy) || errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) {
			finish = FinishOptions{Status: "pending", Code: "deferred", Delay: 10 * time.Second, Deferred: true}
		}
	} else if out.pending {
		finish = FinishOptions{Status: "pending", Code: "deferred", Delay: 10 * time.Second, Deferred: true}
	}
	if ai {
		// AI может уже завершить job при ошибке провайдера; тогда lease утрачен.
		err = interruptWorkerJob(cleanup, lease, finish)
		if errors.Is(err, ErrJobLeaseLost) {
			return executeErr
		}
	} else {
		err = attempt.Finish(cleanup, AttemptFinish{Job: finish})
	}
	if err != nil {
		return errors.Join(executeErr, err)
	}
	if finish.Status == "pending" {
		return nil
	}
	return executeErr
}
