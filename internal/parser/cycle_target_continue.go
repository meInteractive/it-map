package parser

import (
	"context"
	"encoding/json"
)

// PrepareTargetContinuation продолжает подтверждённый запуск без нового Actor.
func (c CycleControl) PrepareTargetContinuation(ctx context.Context) (func(context.Context, string, ScanCandidateScope) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, ErrSettingsStorage
	}
	return func(ctx context.Context, request string, scope ScanCandidateScope) (json.RawMessage, error) {
		if !generationPattern.MatchString(request) {
			return nil, ErrRunInput
		}
		runs := RunRepository{DB: c.DB, Generation: generation}
		verify := func(replay bool) error {
			tx, err := runs.lockGeneration(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			_, err = readCandidateScope(ctx, tx, generation, &scope)
			if err != nil {
				return err
			}
			if replay {
				var job Job
				row := tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM scheduler_jobs j WHERE j.id=(SELECT job_id FROM scheduler_attempts WHERE run_id=? LIMIT 1) AND j.generation=?", runID(generation, request), generation)
				job, err = scanJob(row)
				if err != nil {
					return ErrRunBusy
				}
				if job.ID != scope.ScanJobID || job.Type != "scan_source" || job.CompanyID == nil || *job.CompanyID != scope.CompanyID || job.SourceID == nil || *job.SourceID != "hh" {
					return ErrRunBusy
				}
			}
			if err = tx.Commit(); err != nil {
				return ErrSettingsStorage
			}
			return nil
		}
		if err := verify(false); err != nil {
			return nil, err
		}
		w, err := c.worker(generation)
		if err != nil {
			return nil, err
		}
		w.CandidateScope = nil
		w.ScheduleSources = false
		w.TargetJobID = scope.ScanJobID
		w.DenyActorStarts = true
		w.ResumeJobID = ""
		w.ResumePreviousDataset = false
		result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: request, Trigger: "manual"}, MaxJobs: 1})
		if err != nil {
			return nil, err
		}
		if result.Reused {
			if err = verify(true); err != nil {
				return nil, err
			}
		}
		query, _ := json.Marshal(map[string]string{"requestId": request})
		raw, err := (RunReadRepository{DB: c.DB}).read(ctx, query, generation)
		if err != nil {
			return nil, err
		}
		if !result.Reused {
			return raw, nil
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil {
			return nil, ErrSettingsStorage
		}
		body["reused"] = json.RawMessage("true")
		return json.Marshal(body)
	}, nil
}
