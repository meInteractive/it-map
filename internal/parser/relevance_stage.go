package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"itmap/internal/aiproviders"
	"itmap/internal/backup"
)

var ErrRelevanceChanged = errors.New("vacancy relevance dependencies changed")

// RelevanceStageStore записывает персональную оценку только для владельца цикла.
type RelevanceStageStore struct{ Owner string }

func (s RelevanceStageStore) Prepare(ctx context.Context, tx *sql.Tx, j Job) (AIStagePlan, error) {
	if j.Type != "score_relevance" {
		return AIStagePlan{}, ErrJobInput
	}
	x, err := readRelevanceSnapshot(ctx, tx, j, s.Owner)
	if err != nil {
		return AIStagePlan{}, err
	}
	plan := AIStagePlan{Request: aiproviders.GenerationRequest{Task: "relevance"}, Config: x.Config, provenance: x}
	if stageHash(x.BaseKey) != j.DedupeKey {
		return AIStagePlan{}, ErrRelevanceChanged
	}
	if x.Score.Valid {
		var raw []byte
		err = tx.QueryRowContext(ctx, "SELECT result_json FROM scheduler_attempts WHERE job_id=? AND status='completed' ORDER BY finished_at DESC,id DESC LIMIT 1 FOR UPDATE", j.ID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return AIStagePlan{}, ErrSettingsStorage
		}
		if err == nil {
			var receipt struct {
				BaseHash  string `json:"relevanceInputHash"`
				RolesHash string `json:"relevanceRolesHash"`
			}
			if json.Unmarshal(raw, &receipt) != nil {
				return AIStagePlan{}, ErrRelevanceChanged
			}
			if receipt.BaseHash != "" && (receipt.BaseHash != x.BaseHash || receipt.RolesHash != x.RolesHash) {
				return AIStagePlan{}, ErrRelevanceChanged
			}
		}
		content, _ := json.Marshal(map[string]int64{"relevance": x.Score.Int64})
		plan.Cached = &AIStageOutput{Generation: aiproviders.AIResult{Model: "cached", Message: aiproviders.Message{Content: string(content)}}}
		return plan, nil
	}
	if !x.Eligible {
		return AIStagePlan{}, ErrRelevanceChanged
	}
	plan.Request, err = prepareRelevanceRequest(x.Prompt, x.Resume, x.Title, x.Markdown, x.SalaryFrom, x.SalaryTo)
	if err != nil {
		return AIStagePlan{}, err
	}
	return plan, nil
}
func (s RelevanceStageStore) Revalidate(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	old, ok := plan.provenance.(relevanceSnapshot)
	if !ok {
		return ErrJobInput
	}
	current, err := readRelevanceSnapshot(ctx, tx, j, s.Owner)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, old) {
		return ErrRelevanceChanged
	}
	if stageHash(current.BaseKey) != j.DedupeKey || !current.Score.Valid && !current.Eligible {
		return ErrRelevanceChanged
	}
	return nil
}
func (s RelevanceStageStore) Save(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan, out AIStageOutput) error {
	x, ok := plan.provenance.(relevanceSnapshot)
	if !ok || x.Score.Valid || out.Generation.Model == "" {
		return ErrJobInput
	}
	score, valid := parseRelevanceAnswer(out.Generation.Message.Content)
	if !valid {
		return ErrSnapshotInvalid
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at) VALUES(?,?,?,CURRENT_TIMESTAMP) ON DUPLICATE KEY UPDATE updated_at=IF(relevance IS NULL,CURRENT_TIMESTAMP,updated_at),relevance=IF(relevance IS NULL,VALUES(relevance),relevance)`, x.Owner, x.VacancyID, score)
	if err != nil {
		return ErrSettingsStorage
	}
	var stored sql.NullInt64
	if tx.QueryRowContext(ctx, "SELECT relevance FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=? FOR UPDATE", x.Owner, x.VacancyID).Scan(&stored) != nil || !stored.Valid || stored.Int64 != int64(score) {
		return ErrRelevanceChanged
	}
	return nil
}

// enqueueHHRelevance вызывается после импорта в той же транзакции.
func enqueueHHRelevance(ctx context.Context, tx *sql.Tx, j Job, owner string) error {
	x, err := readRelevanceSnapshot(ctx, tx, j, owner)
	if err != nil {
		return err
	}
	if !x.Eligible {
		return nil
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != owner {
		return ErrCandidateChanged
	}
	priority, ok := settings.Priorities["score_relevance"]
	if !ok || priority < 0 || priority > 100 {
		return ErrSettingsStorage
	}
	payload, err := json.Marshal(map[string]any{"vacancyId": x.VacancyID, "ownerName": owner})
	if err != nil {
		return ErrJobInput
	}
	payload, err = backup.StringifyParsedJSON(payload)
	if err != nil {
		return ErrJobInput
	}
	id, err := stageToken()
	if err != nil {
		return err
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return ErrSettingsStorage
	}
	dedupe := stageHash(x.Key)
	_, err = tx.ExecContext(ctx, `INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,'score_relevance',?,NULL,?,'hh',?,?,?,4,?,?) ON DUPLICATE KEY UPDATE id=id`, id, j.Generation, dedupe, j.CompanyID, j.CandidateID, string(payload), priority, now, now, now)
	if err != nil {
		return ErrSettingsStorage
	}
	var kind, source string
	var company, candidate sql.NullInt64
	var agency sql.NullInt64
	var stored []byte
	err = tx.QueryRowContext(ctx, "SELECT type,company_id,agency_id,candidate_id,source_id,payload FROM scheduler_jobs WHERE generation=? AND dedupe_key=? FOR UPDATE", j.Generation, dedupe).Scan(&kind, &company, &agency, &candidate, &source, &stored)
	if err != nil {
		return ErrSettingsStorage
	}
	stored, err = backup.StringifyParsedJSON(stored)
	if err != nil || kind != "score_relevance" || !company.Valid || company.Int64 != x.CompanyID || agency.Valid || !candidate.Valid || candidate.Int64 != x.CandidateID || source != "hh" || string(stored) != string(payload) {
		return ErrRelevanceChanged
	}
	return nil
}
