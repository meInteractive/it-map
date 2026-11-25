package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/v39"
	"time"
)

// preservedTables is the destination read set shared by historical planning.
// A historical validation result is never a reusable write plan.
func preservedTables(version int) map[string]bool {
	names := map[string]bool{}
	for _, spec := range []struct {
		before int
		tables []string
	}{
		{18, []string{"user_profiles"}}, {19, []string{"openrouter_settings", "openrouter_ai_logs"}}, {20, []string{"system_prompts"}}, {21, []string{"vacancy_cover_letters"}},
		{22, []string{"openrouter_model_metrics"}}, {23, []string{"prompt_model_settings"}}, {24, []string{"openai_settings"}}, {28, []string{"vacancy_roles", "vacancy_role_classification"}},
		{30, []string{"firecrawl_settings"}}, {36, []string{"apify_settings"}}, {39, []string{"parser_error_daily", "parser_candidate_intakes"}},
	} {
		if version < spec.before {
			for _, n := range spec.tables {
				names[n] = true
			}
		}
	}
	return names
}
func readPreserved(ctx context.Context, tx *sql.Tx, actual map[string]sqlTable, wanted map[string]bool, locking bool) ([]Table, error) {
	byName := map[string]v39.Table{}
	for _, t := range v39.Trusted().Tables {
		byName[t.Name] = t
	}
	out := []Table{}
	budget := &exportBudget{}
	// Same stable parent-first order for all restores. FOR UPDATE is a current
	// read under RR and retains row/gap locks through DELETE/INSERT/COMMIT.
	for _, name := range parentFirstTables() {
		if !wanted[name] {
			continue
		}
		d := byName[name]
		rows, _, e := readRowsMode(ctx, tx, actual[name], restoreColumns(d), budget, true, locking)
		if e != nil {
			return nil, e
		}
		t := Table{Name: name, Rows: []map[string]json.RawMessage{}}
		for _, raw := range rows {
			var row map[string]json.RawMessage
			if e = json.Unmarshal(raw, &row); e != nil {
				return nil, e
			}
			t.Rows = append(t.Rows, row)
		}
		out = append(out, t)
	}
	return out, nil
}

// ValidateRestore plans full v14–39 against a read-only consistent snapshot.
// It neither prepares counters nor claims locks for the later write request.
func (r Repository) ValidateRestore(ctx context.Context, data []byte) (RestoreValidation, error) {
	p, e := validateRestoreInput(data)
	if e != nil {
		return RestoreValidation{}, e
	}
	wanted := preservedRestoreTables(p)
	now := time.Now()
	initial, e := prepareHistoricalPayload(p, nil, now)
	if e != nil {
		return RestoreValidation{}, e
	}
	if e = validateRestoreScalars(initial); e != nil {
		return RestoreValidation{}, e
	}
	if r.DB == nil {
		return RestoreValidation{}, errors.New("restore storage unavailable")
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return RestoreValidation{}, e
	}
	defer tx.Rollback()
	actual, e := completeRestoreInventory(ctx, tx)
	if e != nil {
		return RestoreValidation{}, e
	}
	current, e := readPreserved(ctx, tx, actual, wanted, false)
	if e != nil {
		return RestoreValidation{}, e
	}
	plan, e := PrepareRestore(data, current, now)
	if e != nil {
		return RestoreValidation{}, e
	}
	if e = validateRestoreScalars(plan); e != nil {
		return RestoreValidation{}, e
	}
	if e = tx.Commit(); e != nil {
		return RestoreValidation{}, e
	}
	return RestoreValidation{Tables: len(plan.Tables), Rows: plan.Rows, Warnings: plan.Warnings}, nil
}

// Restore imports full v14–39 on a prepared schema. Preliminary planning uses
// no preserved rows: incoming/generated IDs are destination-independent. Existing
// AUTO_INCREMENT/MAX is fenced separately before the data TX. Any concurrently
// allocated preserved IDs have already advanced their own engine high-water.
// The final plan is rebuilt from current locked rows AFTER the generation lock.
func (r Repository) Restore(ctx context.Context, data []byte) (RestoreResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	now := time.Now()
	payload, e := validateRestoreInput(data)
	if e != nil {
		return RestoreResult{}, e
	}
	wanted := preservedRestoreTables(payload)
	plan, e := prepareHistoricalPayload(payload, nil, now)
	if e != nil {
		return RestoreResult{}, e
	}
	session, e := r.openRestorePlan(ctx, plan)
	if e != nil {
		return RestoreResult{}, e
	}
	defer session.Close()
	if plan.SourceVersion == 39 {
		return applyRestore(ctx, session, nil)
	}
	return applyRestore(ctx, session, func(ctx context.Context, tx *sql.Tx, actual map[string]sqlTable) (*RestorePlan, error) {
		current, e := readPreserved(ctx, tx, actual, wanted, true)
		if e != nil {
			return nil, e
		}
		return PrepareRestore(data, current, now)
	})
}
