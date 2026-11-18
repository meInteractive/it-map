package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"regexp"
	"time"
)

var ErrAIStageUnavailable = errors.New("scheduler AI stage is not configured")

// План читается серверным repository, а не из HTTP body или job.payload.
// Store повторно проверяет provenance, scope, настройки и hash перед Save.
type AIStagePlan struct {
	provenance     any
	PreflightError error
	Request        aiproviders.GenerationRequest
	Config         profiles.ModelInput
	Title          string
	TitleRoles     []aiproviders.TitleRole
	Cached         *AIStageOutput
}
type AIStageOutput struct {
	Generation     aiproviders.AIResult
	Classification *aiproviders.TitleResult
	Reused         bool
}
type AIStageStore interface {
	Prepare(context.Context, *sql.Tx, Job) (AIStagePlan, error)
	Revalidate(context.Context, *sql.Tx, Job, AIStagePlan) error
	Save(context.Context, *sql.Tx, Job, AIStagePlan, AIStageOutput) error
}
type AIStageFailureStore interface {
	SaveFailure(context.Context, *sql.Tx, Job, AIStagePlan, error) error
}
type AIStageAbortStore interface {
	Abort(context.Context, *sql.Tx, Job, AIStagePlan) error
}
type AIStageGenerator interface {
	Automatic(context.Context, aiproviders.GenerationRequest, profiles.ModelInput, aiproviders.Billing) (aiproviders.AIResult, error)
}
type AIStageClassifier interface {
	ClassifyTitles(context.Context, string, []aiproviders.TitleRole, aiproviders.Billing, func(context.Context) error) (aiproviders.TitleResult, error)
}
type AIStageExecutor struct {
	Store              AIStageStore
	Generator          AIStageGenerator
	Classifier         AIStageClassifier
	Handoff            *CandidateHandoff
	DeferUnspentBudget bool
	StopClaims         <-chan struct{} // nil сохраняет работу вне непрерывного worker
}

var errBudgetPriorEvidence = errors.New("budget deferral requires untouched job")

func aiStageTask(kind string) string {
	switch kind {
	case "classify_title":
		return "title_classification"
	case "extract_vacancy":
		return "vacancy_extraction"
	case "assign_roles":
		return "vacancy_role"
	case "score_relevance":
		return "relevance"
	}
	return ""
}

// Execute принимает только уже полученную lease. Самостоятельно очередь не запускает.
func (e *AIStageExecutor) Execute(ctx context.Context, lease *JobLease) (AIStageOutput, error) {
	if e == nil || e.Store == nil || lease == nil {
		return AIStageOutput{}, ErrAIStageUnavailable
	}
	if claimsStopped(e.StopClaims) {
		return AIStageOutput{}, ErrWorkerDrainBeforePaid
	}
	job := lease.Job()
	task := aiStageTask(job.Type)
	if task == "" {
		return AIStageOutput{}, ErrAIStageUnavailable
	}
	var plan AIStagePlan
	err := lease.WithinOwned(ctx, func(tx *sql.Tx) error { var err error; plan, err = e.Store.Prepare(ctx, tx, job); return err })
	if err != nil {
		return AIStageOutput{}, err
	}
	if plan.Request.Task != task {
		return AIStageOutput{}, ErrJobInput
	}
	abort := func() error {
		if store, ok := e.Store.(AIStageAbortStore); ok {
			return lease.WithinOwned(ctx, func(tx *sql.Tx) error { return store.Abort(ctx, tx, job, plan) })
		}
		return nil
	}
	// Пока шёл Prepare, корневой worker мог получить SIGTERM, но grace
	// сохраняет step context активным. Без нового платного допуска вернём job.
	if claimsStopped(e.StopClaims) {
		return AIStageOutput{}, errors.Join(ErrWorkerDrainBeforePaid, abort())
	}
	if plan.Cached == nil && plan.PreflightError == nil && ((job.Type == "classify_title" && e.Classifier == nil) || (job.Type != "classify_title" && e.Generator == nil)) {
		return AIStageOutput{}, errors.Join(ErrAIStageUnavailable, abort())
	}
	attempt, err := lease.StartAttempt(ctx)
	if err != nil {
		return AIStageOutput{}, errors.Join(err, abort())
	}
	// StartAttempt мог завершить COMMIT уже после сигнала остановки.
	// Здесь ещё нет платного резерва и провайдер не вызывался.
	if claimsStopped(e.StopClaims) {
		return AIStageOutput{}, ErrWorkerDrainBeforePaid
	}
	check := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if plan.Request.CheckActive != nil {
			if err := plan.Request.CheckActive(ctx); err != nil {
				return err
			}
		}
		return lease.WithinOwned(ctx, func(tx *sql.Tx) error { return e.Store.Revalidate(ctx, tx, job, plan) })
	}
	if err = check(ctx); err != nil {
		return AIStageOutput{}, err
	}
	// Проверяем StopClaims непосредственно перед каждым платным Billing.Run,
	// а не внутри CheckActive: после начатого вызова graceful shutdown
	// должен дать записать ответ, стоимость и результат до истечения grace.
	var billing aiproviders.Billing = attempt
	if e.StopClaims != nil {
		billing = workerStopBilling{delegate: attempt, stop: e.StopClaims}
	}
	output := AIStageOutput{}
	if plan.PreflightError != nil {
		err = plan.PreflightError
	} else if plan.Cached != nil {
		output = *plan.Cached
		output.Reused = true
	} else {
		if job.Type == "classify_title" {
			var result aiproviders.TitleResult
			result, err = e.Classifier.ClassifyTitles(ctx, plan.Title, plan.TitleRoles, billing, check)
			output.Classification = &result
		} else {
			request := plan.Request
			request.CheckActive = check
			output.Generation, err = e.Generator.Automatic(ctx, request, plan.Config, billing)
		}
	}
	if err != nil {
		// Штатная остановка до первого платного Billing.Run не является
		// неуспешным AI-результатом. Worker вернёт lease в pending без POST.
		if errors.Is(err, ErrWorkerDrainBeforePaid) && attempt.Requests() == 0 && !attempt.Unknown() {
			return AIStageOutput{}, err
		}
		if e.DeferUnspentBudget && (errors.Is(err, ErrBudgetLimit) || errors.Is(err, ErrHistoricalAIUnresolved)) && attempt.Requests() == 0 && !attempt.Unknown() {
			if store, ok := e.Store.(AIStageAbortStore); ok {
				code, message := "budget", "Ожидание доступного AI-бюджета."
				if errors.Is(err, ErrHistoricalAIUnresolved) {
					code, message = "historical_ai_unknown", "Ожидание проверки прежних платных AI-запросов."
				}
				deferErr := attempt.FinishWithin(ctx, AttemptFinish{Job: FinishOptions{Status: "pending", Code: code, Message: message, Deferred: true, Delay: 30 * time.Minute}}, func(tx *sql.Tx) error {
					if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
						return err
					}
					var unsafe int
					if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND id<>? AND NOT (status='pending' AND reason_code IN ('budget','historical_ai_unknown') AND reserved_usd=0 AND COALESCE(cost_usd,-1)=0)`, job.ID, attempt.ID()).Scan(&unsafe); err != nil {
						return storageError(ctx)
					}
					if unsafe != 0 {
						return errBudgetPriorEvidence
					}
					return store.Abort(ctx, tx, job, plan)
				})
				if deferErr == nil {
					return AIStageOutput{}, nil
				}
				if !errors.Is(deferErr, errBudgetPriorEvidence) {
					return AIStageOutput{}, deferErr
				}
			}
		}
		// Неизвестный платный исход остаётся зарезервированным; автоматического POST retry нет.
		finish := e.finishFailed(ctx, attempt, job, plan, err)
		if finish != nil {
			return AIStageOutput{}, finish
		}
		return AIStageOutput{}, err
	}
	if err = check(ctx); err != nil {
		return AIStageOutput{}, err
	}
	var stageError error
	finish := FinishOptions{Status: "completed"}
	if output.Classification != nil {
		for _, r := range output.Classification.Results {
			if r.Error != nil || r.Decision == nil {
				finish = FinishOptions{Status: "failed", Code: "invalid_output", Message: "Нет актуальных решений JEV для всех направлений."}
				stageError = &aiproviders.ServiceError{Code: finish.Code, Message: finish.Message, Status: 502}
				break
			}
		}
	}
	receipt, _ := json.Marshal(map[string]any{"model": output.Generation.Model, "reused": output.Reused})
	if x, ok := plan.provenance.(relevanceSnapshot); ok && job.Type == "score_relevance" {
		receipt, _ = json.Marshal(map[string]any{"model": output.Generation.Model, "reused": output.Reused, "relevanceInputHash": x.BaseHash, "relevanceRolesHash": x.RolesHash})
	}
	if output.Classification != nil {
		receipt, _ = json.Marshal(map[string]any{"model": output.Classification.Model, "reused": output.Reused})
	}
	// Save may perform a second, deterministic extraction validation inside
	// the owned transaction. If it rejects model output, FinishWithin rolls
	// back without attempting a COMMIT. Only in this precisely identified case
	// may we finalize the SAME paid attempt with a failure receipt. SQL/storage
	// errors, provenance errors, and unknown COMMIT outcomes must NOT be retried.
	var rejectedOutput error
	err = attempt.FinishWithin(ctx, AttemptFinish{Job: finish, Result: receipt}, func(tx *sql.Tx) error {
		if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
			return err
		}
		if !output.Reused {
			if saveErr := e.Store.Save(ctx, tx, job, plan, output); saveErr != nil {
				var service *aiproviders.ServiceError
				if job.Type == "extract_vacancy" && errors.As(saveErr, &service) &&
					service.Code == "invalid_output" && service.Status == 422 &&
					extractionOutputIssue(saveErr) != "" {
					rejectedOutput = saveErr
				}
				return saveErr
			}
		}
		if stageError == nil && e.Handoff != nil {
			return e.Handoff.Enqueue(ctx, tx, job)
		}
		return nil
	})
	if rejectedOutput != nil && errors.Is(err, rejectedOutput) {
		// There was no successful FinishWithin COMMIT: the callback itself
		// rejected the value. Record a bounded extraction failure and preserve
		// the already-settled or still-unknown paid reserve atomically.
		if finishErr := e.finishFailed(ctx, attempt, job, plan, rejectedOutput); finishErr != nil {
			return AIStageOutput{}, finishErr
		}
		return AIStageOutput{}, rejectedOutput
	}
	if err != nil {
		return AIStageOutput{}, err
	}
	return output, stageError
}

// finishFailed commits a failure receipt and the job transition together.
// It does not run a provider request or reconcile unknown monetary outcomes.
func (e *AIStageExecutor) finishFailed(ctx context.Context, attempt *Attempt, job Job, plan AIStagePlan, cause error) error {
	code := aiStageErrorCode(cause)
	return attempt.FinishWithin(ctx, AttemptFinish{
		Job:    FinishOptions{Status: "failed", Code: code, Message: "Не удалось выполнить AI-этап. Проверьте причину перед повторным запуском."},
		Result: json.RawMessage(`{}`),
	}, func(tx *sql.Tx) error {
		if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
			return err
		}
		if store, ok := e.Store.(AIStageFailureStore); ok {
			return store.SaveFailure(ctx, tx, job, plan, cause)
		}
		return nil
	})
}

var aiStageCode = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

func aiStageErrorCode(err error) string {
	var service *aiproviders.ServiceError
	var provider *aihttp.Error
	code := "internal"
	switch {
	case errors.As(err, &service):
		code = service.Code
	case errors.As(err, &provider):
		code = provider.Code
	case errors.Is(err, ErrBudgetLimit):
		code = "budget"
	case errors.Is(err, ErrMoneyUncertain):
		code = "money_uncertain"
	case errors.Is(err, ErrHistoricalAIUnresolved):
		code = "historical_ai_unknown"
	case errors.Is(err, context.Canceled):
		code = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	}
	if !aiStageCode.MatchString(code) {
		return "internal"
	}
	return code
}
