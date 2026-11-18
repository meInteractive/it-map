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
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/retryafter"
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
	EnforceActorCooldown   bool
	DeferUnspentBudget     bool
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
	QueueProfile string          // "" = прежний ручной цикл; balanced, launch, reconcile, pipeline
	StopClaims   <-chan struct{} // необязательный сигнал остановки перед следующим claim
	// MinRemaining reserves time before any new claim, including the first.
	// Zero retains the existing behaviour for manual cycles.
	MinRemaining time.Duration
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
	// Не допускаем runtime, обходящий общий ledger/cooldown worker.
	if w.EnforceActorCooldown {
		switch w.ListingRuntime.(type) {
		case nil, apifyruns.Service, *apifyruns.Service:
		default:
			return result, ErrRunInput
		}
		switch w.DetailRuntime.(type) {
		case nil, apifyruns.Service, *apifyruns.Service:
		default:
			return result, ErrRunInput
		}
	}
	if !validTargetedCycle(w, options) {
		return result, ErrRunInput
	}
	if claimsStopped(options.StopClaims) {
		return result, context.Canceled
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
	if duration < time.Millisecond || duration > 110*time.Second || options.MinRemaining < 0 || options.MinRemaining > duration {
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
		if claimsStopped(options.StopClaims) {
			return context.Canceled
		}
		return admitHHActorStart(w.ResumePreviousDataset || w.DenyActorStarts, actorStarts.Load())
	}
	denyNew := func() error {
		if claimsStopped(options.StopClaims) {
			return context.Canceled
		}
		if w.DenyActorStarts {
			return ErrHHCycleLimit
		}
		return nil
	}
	count := func() { actorStarts.Add(1) }
	service := apifyruns.Service{Repository: apifyruns.Repository{DB: w.Settings.DB, Settings: w.Settings, TrustFreeProviderLimit: w.TrustFreeProviderLimit, EnforceActorCooldown: w.EnforceActorCooldown}, BeforeNewOperation: denyNew, BeforeReserve: admit, AfterReserve: count}
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
		existing.Repository.EnforceActorCooldown = existing.Repository.EnforceActorCooldown || w.EnforceActorCooldown
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
		existing.Repository.EnforceActorCooldown = existing.Repository.EnforceActorCooldown || w.EnforceActorCooldown
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
		ai:       AIStageDispatcher{Owner: owner, Generator: w.Generator, Classifier: w.Classifier, DeferUnspentBudget: w.DeferUnspentBudget},
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
			if claimsStopped(options.StopClaims) {
				return result, nil
			}
			deadline, _ := bounded.Deadline()
			if !enoughTimeForNextJob(time.Until(deadline), options.MinRemaining) {
				return result, nil
			}
			processed, stepErr := w.cycleStep(ctx, bounded, options.StopClaims, options.MinRemaining, start.Run.ID, phase, d, owner, &result, &phaseProcessed, &hhStages, &status, &releaseRun)
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

// Бюджет проверяется перед любым claim, включая первый: при нехватке
// времени новый цикл сможет взять задание без близкой отмены контекста.
// Нулевой minimum сохраняет поведение ручных запусков.
func enoughTimeForNextJob(remaining, minimum time.Duration) bool {
	return minimum == 0 || remaining >= minimum
}

func claimsStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// ClaimJob may spend most of the remaining budget selecting and locking a job.
// A pre-claim check cannot authorize the subsequent external work, and
// SIGTERM may close StopClaims while ClaimJob is in flight.
func admittedAfterClaim(now, cycleDeadline, workDeadline time.Time, minRemaining time.Duration, stopClaims <-chan struct{}) bool {
	return !claimsStopped(stopClaims) && now.Before(workDeadline) &&
		enoughTimeForNextJob(cycleDeadline.Sub(now), minRemaining)
}

// pipelineCleanupReserve даёт bounded cleanup (до 10 секунд) шанс
// завершиться до срока job lease при дедлайне обработки.
const pipelineCleanupReserve = 12 * time.Second

// Отсеиваем поздний claim до создания lease и attempt; внешний цикл
// остаётся владельцем оригинального дедлайна для recovery/reportCycle.
func pipelineWorkDeadline(now, cycleDeadline time.Time) (time.Time, bool) {
	cutoff := cycleDeadline.Add(-pipelineCleanupReserve)
	return cutoff, now.Before(cutoff)
}

// cycleStep выполняет один claim и обработку с прежними ограничениями run.
func (w Worker) cycleStep(ctx, bounded context.Context, stopClaims <-chan struct{}, minRemaining time.Duration, runID, phase string, d workerDispatch, owner string, result *WorkerResult, phaseProcessed, hhStages *int, status *string, releaseRun *bool) (bool, error) {
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
	// У AI-стадии должен остаться отдельный интервал на запись результата
	// или безопасное завершение attempt/job при отмене контекста провайдера.
	// Run и job lease продолжают жить до исходной границы bounded.
	workDeadline := deadline
	if phase == "pipeline" {
		var admissible bool
		workDeadline, admissible = pipelineWorkDeadline(time.Now(), deadline)
		if !admissible {
			return false, nil
		}
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
		claim.WorkerPipelineFairness = true
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
	// A claim is not permission for a paid provider operation. Recheck both
	// the time budget and shutdown before creating even the first attempt:
	// DB contention can consume the pre-claim minimum, and the worker's grace
	// context can remain active after StopClaims closes. Return a verified
	// lease to pending without consuming an attempt or running provider I/O.
	if bounded.Err() != nil || !admittedAfterClaim(time.Now(), deadline, workDeadline, minRemaining, stopClaims) {
		cleanup, stopCleanup := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stopCleanup()
		if err := interruptWorkerJob(cleanup, lease, FinishOptions{
			Status: "pending", Code: "deferred", Delay: 10 * time.Second, Deferred: true,
		}); err != nil {
			// Never publish a completed run after a failed ownership-checked
			// rollback. Normal lease recovery must reconcile this claim.
			*releaseRun = false
			*status = "partial"
			return false, err
		}
		return false, nil
	}
	result.Processed++
	(*phaseProcessed)++
	if kind := lease.Job().Type; kind == "scan_source" || kind == "fetch_full_text" {
		(*hhStages)++
	}
	// Провайдер и AI-этап ограничены ранним дедлайном; cleanup в
	// workerExecute имеет собственный контекст и проверяет действующую lease.
	workCtx, stopWork := context.WithDeadline(bounded, workDeadline)
	executeErr := workerExecute(workCtx, d, lease, owner, w.DenyActorStarts, w.DeferUnspentBudget && w.EnforceActorCooldown)
	stopWork()
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

func workerExecute(ctx context.Context, d workerDispatch, lease *JobLease, owner string, denyActorStarts, deferUnspent bool) error {
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
		// Публичный GET безопасно повторить. Временный сбой не должен
		// превращаться в новый платный fallback или расходовать попытку.
		if kind == "fetch_full_text" && onlyTemporaryPublicHH(executeErr) {
			delay := hhpublic.MinRetryDelay
			var retry *hhpublic.TemporaryError
			if errors.As(executeErr, &retry) && retry != nil && retry.RetryAfter >= hhpublic.MinRetryDelay && retry.RetryAfter <= hhpublic.MaxRetryDelay {
				delay = retry.RetryAfter
			}
			previous, retryErr := recentReadFailures(cleanup, lease, attempt.ID(), hhReadRetryReason)
			if retryErr != nil {
				return errors.Join(executeErr, retryErr)
			}
			delay = readBackoff(previous, delay)
			finish = FinishOptions{Status: "pending", Code: hhReadRetryReason, Delay: spreadDeferredRetry(lease.Job().ID, "hh-public", delay, hhpublic.MaxRetryDelay), Deferred: true}
		}
		if errors.Is(executeErr, ErrHHRoleFilter) {
			finish = FinishOptions{Status: "failed", Code: "hh_roles_missing", Message: "Выберите профессиональные роли HH для каждого активного направления и повторите запуск."}
		}
		if onlyAdmissionCause(executeErr, ErrHHDetailPending) || onlyAdmissionCause(executeErr, ErrDetailBusy) || onlyAdmissionCause(executeErr, ErrHHCycleLimit) || onlyAdmissionCause(executeErr, context.Canceled) || onlyAdmissionCause(executeErr, context.DeadlineExceeded) {
			finish = FinishOptions{Status: "pending", Code: "deferred", Delay: 10 * time.Second, Deferred: true}
		}
		if denyActorStarts && onlyAdmissionCause(executeErr, ErrHHCycleLimit) {
			// Частые pipeline-сессии не должны непрерывно повторять платный fallback.
			finish = FinishOptions{Status: "pending", Code: "paid_start_disabled", Delay: 15 * time.Minute, Deferred: true}
		}
		// Эти ошибки Service возникают до резерва и POST. Повтор допустим
		// только у worker, использующего проверенный runtime и paid ledger.
		if deferUnspent && (kind == "scan_source" || kind == "fetch_full_text") {
			if onlyAdmissionCause(executeErr, apifyruns.ErrBusy) {
				finish = FinishOptions{Status: "pending", Code: "deferred", Delay: 30 * time.Second, Deferred: true}
			}
			if onlyAdmissionCause(executeErr, apifyruns.ErrBudget) {
				finish = FinishOptions{Status: "pending", Code: "budget", Delay: 30 * time.Minute, Deferred: true}
			}
		}
		// Провайдер может отклонить GET из-за бюджета; это не разрешает
		// менять профиль известной операции и запускать новый Actor.
		if (kind == "scan_source" || kind == "fetch_full_text") && onlyAdmissionCause(executeErr, apifyruns.ErrReadBudget) {
			delay := 30 * time.Minute
			var hint *apifyruns.ReadBudgetError
			if errors.As(executeErr, &hint) && hint != nil && hint.RetryAfter > delay && hint.RetryAfter <= retryafter.MaxDelay {
				delay = hint.RetryAfter
			}
			finish = FinishOptions{Status: "pending", Code: "budget", Delay: delay, Deferred: true}
		}
		// Повторяем только типизированную ошибку безопасного GET; POST
		// с неизвестным результатом остаётся в отдельном состоянии.
		if (kind == "scan_source" || kind == "fetch_full_text") && onlyAdmissionCause(executeErr, apifyruns.ErrReadTemporary) {
			delay := retryafter.MinDelay
			var hint *apifyruns.ReadTemporaryError
			if errors.As(executeErr, &hint) && hint != nil && hint.RetryAfter >= retryafter.MinDelay && hint.RetryAfter <= retryafter.MaxDelay {
				delay = hint.RetryAfter
			}
			previous, retryErr := recentReadFailures(cleanup, lease, attempt.ID(), apifyReadRetryReason)
			if retryErr != nil {
				return errors.Join(executeErr, retryErr)
			}
			delay = readBackoff(previous, delay)
			finish = FinishOptions{Status: "pending", Code: apifyReadRetryReason, Delay: spreadDeferredRetry(lease.Job().ID, "apify-read", delay, retryafter.MaxDelay), Deferred: true}
		}
		if onlyAdmissionCause(executeErr, apifyruns.ErrCooldown) {
			delay := 15 * time.Minute
			var cooldown *apifyruns.CooldownError
			if errors.As(executeErr, &cooldown) && cooldown != nil && cooldown.RetryAfter > 0 && cooldown.RetryAfter <= 15*time.Minute {
				delay = cooldown.RetryAfter
			}
			// Даём запас на разницу часов и планирование; Reserve проверит снова.
			finish = FinishOptions{Status: "pending", Code: "deferred", Delay: delay + time.Second, Deferred: true}
		}
		if onlyAdmissionCause(executeErr, apifyruns.ErrHistoricalUnresolved) {
			finish = FinishOptions{Status: "pending", Code: "historical_paid_unknown", Delay: 30 * time.Minute, Deferred: true}
		}
		if onlyAdmissionCause(executeErr, apifyruns.ErrCurrentUnresolved) {
			finish = FinishOptions{Status: "pending", Code: "current_paid_unknown", Delay: 30 * time.Minute, Deferred: true}
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
