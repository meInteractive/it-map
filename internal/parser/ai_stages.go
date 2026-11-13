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
	if plan.Cached == nil && plan.PreflightError == nil && ((job.Type == "classify_title" && e.Classifier == nil) || (job.Type != "classify_title" && e.Generator == nil)) {
		return AIStageOutput{}, errors.Join(ErrAIStageUnavailable, abort())
	}
	attempt, err := lease.StartAttempt(ctx)
	if err != nil {
		return AIStageOutput{}, errors.Join(err, abort())
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
	output := AIStageOutput{}
	if plan.PreflightError != nil {
		err = plan.PreflightError
	} else if plan.Cached != nil {
		output = *plan.Cached
		output.Reused = true
	} else {
		if job.Type == "classify_title" {
			var result aiproviders.TitleResult
			result, err = e.Classifier.ClassifyTitles(ctx, plan.Title, plan.TitleRoles, attempt, check)
			output.Classification = &result
		} else {
			request := plan.Request
			request.CheckActive = check
			output.Generation, err = e.Generator.Automatic(ctx, request, plan.Config, attempt)
		}
	}
	if err != nil {
		if e.DeferUnspentBudget && errors.Is(err, ErrBudgetLimit) && attempt.Requests() == 0 && !attempt.Unknown() {
			if store, ok := e.Store.(AIStageAbortStore); ok {
				deferErr := attempt.FinishWithin(ctx, AttemptFinish{Job: FinishOptions{Status: "pending", Code: "budget", Message: "Ожидание доступного AI-бюджета.", Deferred: true, Delay: 30 * time.Minute}}, func(tx *sql.Tx) error {
					if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
						return err
					}
					var unsafe int
					if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND id<>? AND NOT (status='pending' AND reason_code='budget' AND reserved_usd=0 AND COALESCE(cost_usd,-1)=0)`, job.ID, attempt.ID()).Scan(&unsafe); err != nil {
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
		code := aiStageErrorCode(err)
		finish := attempt.FinishWithin(ctx, AttemptFinish{Job: FinishOptions{Status: "failed", Code: code, Message: "Не удалось выполнить AI-этап. Проверьте причину перед повторным запуском."}, Result: json.RawMessage(`{}`)}, func(tx *sql.Tx) error {
			if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
				return err
			}
			if store, ok := e.Store.(AIStageFailureStore); ok {
				return store.SaveFailure(ctx, tx, job, plan, err)
			}
			return nil
		})
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
	err = attempt.FinishWithin(ctx, AttemptFinish{Job: finish, Result: receipt}, func(tx *sql.Tx) error {
		if err := e.Store.Revalidate(ctx, tx, job, plan); err != nil {
			return err
		}
		if !output.Reused {
			if err := e.Store.Save(ctx, tx, job, plan, output); err != nil {
				return err
			}
		}
		if stageError == nil && e.Handoff != nil {
			return e.Handoff.Enqueue(ctx, tx, job)
		}
		return nil
	})
	if err != nil {
		return AIStageOutput{}, err
	}
	return output, stageError
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
