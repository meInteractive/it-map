package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
)

// Worker запускается только явным вызовом Cycle; таймеров и фоновых задач нет.
type Worker struct {
	Runs                   RunRepository
	Settings               apifysettings.Repository
	ListingRuntime         HHListingRuntime
	DetailRuntime          HHDetailRuntime
	Public                 HHPublicFetcher
	Generator              AIStageGenerator
	Classifier             AIStageClassifier
	ScheduleSources        bool
	VerifiedHHOnly         bool
	ContinueOnStageFailure bool
	ResumePreviousDataset  bool
	ResumeJobID            string
	TargetJobID            string
	DenyActorStarts        bool
	TrustFreeProviderLimit bool
	CandidateScope         *ScanCandidateScope
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
	Run          RunOptions
	MaxJobs      int
	QueueProfile string // "" = прежний ручной цикл; balanced, launch, reconcile, pipeline
}
type WorkerResult struct {
	Run       Run
	Processed int
	Reused    bool
}

var ErrHHCycleLimit = errors.New("HH cycle actor start limit reached")

func admitHHActorStart(resumePrevious bool, starts int32) error {
	if resumePrevious || starts >= 1 {
		return ErrHHCycleLimit
	}
	return nil
}

var workerTypes = []string{"scan_source", "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}

// Выбранное задание не должно расширять область цикла или возобновлять dataset.
func validTargetedCycle(w Worker, options WorkerOptions) bool {
	if w.CandidateScope != nil {
		return !w.ScheduleSources && !w.ResumePreviousDataset && w.ResumeJobID == "" && w.TargetJobID == "" && options.MaxJobs == 1
	}
	if w.TargetJobID == "" {
		return true
	}
	return !w.ScheduleSources && !w.ResumePreviousDataset && w.ResumeJobID == "" && options.MaxJobs == 1
}

func (w Worker) Cycle(ctx context.Context, options WorkerOptions) (result WorkerResult, failure error) {
	if w.Runs.DB == nil || w.Settings.DB != w.Runs.DB || options.MaxJobs < 1 || options.MaxJobs > 50 || options.Run.Diagnostic {
		return result, ErrRunInput
	}
	if !validTargetedCycle(w, options) {
		return result, ErrRunInput
	}
	switch options.QueueProfile {
	case "", "balanced", "launch", "reconcile", "pipeline":
	default:
		return result, ErrRunInput
	}
	if options.QueueProfile != "" && (w.ResumeJobID != "" || w.ResumePreviousDataset || w.TargetJobID != "" || w.CandidateScope != nil) {
		return result, ErrRunInput
	}
	duration := options.Run.Duration
	if duration == 0 {
		duration = 110 * time.Second
	}
	if duration < time.Millisecond || duration > 110*time.Second {
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
	releaseRun := true
	defer func() {
		// Ошибка записи может оставить принадлежащие run попытки открытыми.
		// Не закрывать run: после истечения lease стандартное восстановление
		// сохранит резервы и освободит задания.
		if !releaseRun {
			return
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := w.Runs.Finish(finishCtx, start.Run.ID, status); err != nil {
			failure = errors.Join(failure, err)
		} else {
			result.Run.Status = status
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
	if w.ScheduleSources {
		if err := scheduleHH(bounded, w.Runs, w.Settings, start.Run.ID, owner); err != nil {
			status = "partial"
			return result, err
		}
	}
	var actorStarts atomic.Int32
	admit := func() error {
		return admitHHActorStart(w.ResumePreviousDataset || w.DenyActorStarts, actorStarts.Load())
	}
	denyNew := func() error {
		if w.DenyActorStarts {
			return ErrHHCycleLimit
		}
		return nil
	}
	count := func() { actorStarts.Add(1) }
	service := apifyruns.Service{Repository: apifyruns.Repository{DB: w.Settings.DB, Settings: w.Settings, TrustFreeProviderLimit: w.TrustFreeProviderLimit}, BeforeNewOperation: denyNew, BeforeReserve: admit, AfterReserve: count}
	listingRuntime := w.ListingRuntime
	if existing, ok := listingRuntime.(*apifyruns.Service); ok {
		if existing == nil {
			listingRuntime = nil
		} else {
			listingRuntime = *existing
		}
	}
	if listingRuntime == nil {
		listingRuntime = service
	} else if existing, ok := listingRuntime.(apifyruns.Service); ok {
		previousNew := existing.BeforeNewOperation
		existing.BeforeNewOperation = func() error {
			if previousNew != nil {
				if err := previousNew(); err != nil {
					return err
				}
			}
			return denyNew()
		}
		previous := existing.BeforeReserve
		previousAfter := existing.AfterReserve
		existing.AfterReserve = func() {
			if previousAfter != nil {
				previousAfter()
			}
			count()
		}
		existing.BeforeReserve = func() error {
			if previous != nil {
				if err := previous(); err != nil {
					return err
				}
			}
			return admit()
		}
		listingRuntime = existing
	}
	detailRuntime := w.DetailRuntime
	if existing, ok := detailRuntime.(*apifyruns.Service); ok {
		if existing == nil {
			detailRuntime = nil
		} else {
			detailRuntime = *existing
		}
	}
	if detailRuntime == nil {
		detailRuntime = service
	} else if existing, ok := detailRuntime.(apifyruns.Service); ok {
		previousNew := existing.BeforeNewOperation
		existing.BeforeNewOperation = func() error {
			if previousNew != nil {
				if err := previousNew(); err != nil {
					return err
				}
			}
			return denyNew()
		}
		previous := existing.BeforeReserve
		previousAfter := existing.AfterReserve
		existing.AfterReserve = func() {
			if previousAfter != nil {
				previousAfter()
			}
			count()
		}
		existing.BeforeReserve = func() error {
			if previous != nil {
				if err := previous(); err != nil {
					return err
				}
			}
			return admit()
		}
		detailRuntime = existing
	}
	d := workerDispatch{
		listing:  HHListingExecutor{Settings: w.Settings, Runtime: listingRuntime, Owner: owner, ResumePrevious: w.ResumePreviousDataset},
		detail:   HHFullTextExecutor{Settings: w.Settings, Runtime: detailRuntime, Public: w.Public, Owner: owner},
		importer: HHImportExecutor{Settings: w.Settings, Owner: owner},
		ai:       AIStageDispatcher{Owner: owner, Generator: w.Generator, Classifier: w.Classifier},
	}
	if err := w.Runs.reportCycle(bounded, start.Run.ID, 0, true); err != nil {
		status = "partial"
		return result, err
	}
	// Старый ручной запуск сохраняет исходный порядок. Автоматический тик
	// выполняет независимые логические очереди над одной SQL-таблицей:
	// платный запуск, безопасный GET уже сохранённого Actor, AI/импорт.
	phases := []string{options.QueueProfile}
	if options.QueueProfile == "balanced" {
		phases = []string{"launch", "reconcile", "pipeline"}
	}
	for _, phase := range phases {
		phaseLimit := options.MaxJobs
		switch phase {
		case "launch":
			phaseLimit = 1
		case "reconcile":
			phaseLimit = 5
		}
		phaseProcessed := 0
		hhStages := 0
		for result.Processed < options.MaxJobs && phaseProcessed < phaseLimit {
			processed, stepErr := w.cycleStep(ctx, bounded, start.Run.ID, phase, d, owner, &result, &phaseProcessed, &hhStages, &status, &releaseRun)
			if stepErr != nil {
				return result, stepErr
			}
			if !processed {
				break
			}
		}
	}
	return result, nil
}

// cycleStep выполняет один claim и обработку с прежними ограничениями run.
func (w Worker) cycleStep(ctx, bounded context.Context, runID, phase string, d workerDispatch, owner string, result *WorkerResult, phaseProcessed, hhStages *int, status *string, releaseRun *bool) (bool, error) {
	if err := bounded.Err(); err != nil {
		*status = "interrupted"
		return false, err
	}
	source := "hh"
	deadline, _ := bounded.Deadline()
	jobDuration := time.Until(deadline)
	if jobDuration < time.Millisecond {
		*status = "interrupted"
		return false, context.DeadlineExceeded
	}
	allowHH := *hhStages < 5
	claim := ClaimOptions{Types: workerTypes, SourceID: &source, Lease: jobDuration, AllowApify: &allowHH, VerifiedHHOnly: w.ScheduleSources || w.VerifiedHHOnly}
	switch phase {
	case "launch":
		claim.Types = []string{"scan_source"}
		claim.HHScanPhase = "launch"
	case "reconcile":
		claim.Types = []string{"scan_source"}
		claim.HHScanPhase = "reconcile"
	case "pipeline":
		claim.Types = []string{"classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}
	}
	if w.ResumeJobID != "" {
		if !w.ResumePreviousDataset {
			*status = "partial"
			return false, ErrRunInput
		}
		claim.Types = []string{"scan_source"}
		claim.JobID = &w.ResumeJobID
	}
	if w.TargetJobID != "" {
		claim.Types = []string{"scan_source"}
		claim.JobID = &w.TargetJobID
	}
	if w.CandidateScope != nil {
		claim.Types = []string{"classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}
		claim.ScanScope = w.CandidateScope
	}
	lease, err := w.Runs.ClaimJob(bounded, runID, claim)
	if err != nil {
		*status = "partial"
		return false, err
	}
	if lease == nil {
		return false, nil
	}
	result.Processed++
	(*phaseProcessed)++
	if kind := lease.Job().Type; kind == "scan_source" || kind == "fetch_full_text" {
		(*hhStages)++
	}
	executeErr := workerExecute(bounded, d, lease, owner, w.DenyActorStarts)
	reportCtx, reportCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	reportErr := w.Runs.reportCycle(reportCtx, runID, result.Processed, false)
	reportCancel()
	if executeErr != nil || reportErr != nil {
		if executeErr != nil && reportErr == nil && w.ContinueOnStageFailure {
			checkCtx, checkCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			finished, checkErr := w.Runs.finishedWorkerStep(checkCtx, lease)
			checkCancel()
			if checkErr == nil && finished {
				*status = "partial"
				return true, nil
			}
			reportErr = errors.Join(reportErr, checkErr)
		}
		*releaseRun = false
		*status = "partial"
		return false, errors.Join(executeErr, reportErr)
	}
	return true, nil
}

func workerExecute(ctx context.Context, d workerDispatch, lease *JobLease, owner string, denyActorStarts bool) error {
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
	finish := FinishOptions{Status: "completed", Code: out.code}
	if executeErr != nil {
		finish = FinishOptions{Status: "failed", Code: "stage_failed", Message: "Не удалось выполнить этап обработки."}
		if errors.Is(executeErr, ErrHHRoleFilter) {
			finish = FinishOptions{Status: "failed", Code: "hh_roles_missing", Message: "Выберите профессиональные роли HH для каждого активного направления и повторите запуск."}
		}
		if errors.Is(executeErr, ErrHHDetailPending) || errors.Is(executeErr, ErrDetailBusy) || errors.Is(executeErr, ErrHHCycleLimit) || errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) {
			finish = FinishOptions{Status: "pending", Code: "deferred", Delay: 10 * time.Second, Deferred: true}
		}
		if denyActorStarts && errors.Is(executeErr, ErrHHCycleLimit) {
			// Частые pipeline-сессии не должны непрерывно повторять платный fallback.
			finish = FinishOptions{Status: "pending", Code: "paid_start_disabled", Delay: 15 * time.Minute, Deferred: true}
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
		var save func(*sql.Tx) error
		if kind == "scan_source" && finish.Status == "completed" {
			save = func(tx *sql.Tx) error { return completeScheduledHH(cleanup, tx, lease.Job(), owner, out.code) }
		}
		err = attempt.FinishWithin(cleanup, AttemptFinish{Job: finish}, save)
	}
	if err != nil {
		return errors.Join(executeErr, err)
	}
	if finish.Status == "pending" {
		return nil
	}
	return executeErr
}
