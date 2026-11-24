package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"itmap/internal/aiproviders"
	"itmap/internal/profiles"
)

var ErrManualRelevanceMissing = errors.New("vacancy not found")
var ErrManualRelevanceBusy = errors.New("manual relevance request unresolved")

type ManualRelevanceService struct {
	DB        *sql.DB
	Generator AIStageGenerator
}

type ManualRelevanceResult struct {
	VacancyID int64  `json:"vacancyId"`
	Relevance int    `json:"relevance"`
	Model     string `json:"model"`
}

type manualRelevanceInput struct {
	generation, snapshot, title, body, resume, prompt string
	from, to                                          sql.NullInt64
	config                                            profiles.ModelInput
}

type relevanceQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readManualRelevance(ctx context.Context, q relevanceQuery, user string, id int64) (manualRelevanceInput, error) {
	var v manualRelevanceInput
	query := `SELECT g.generation,
		JSON_ARRAY(v.created_at,v.title,v.description_md,v.salary_from,v.salary_to,p.resume_md,p.revision,
			sp.markdown,sp.revision,m.provider,m.openai_model,m.openrouter_model,m.reasoning_effort,m.revision,
			IF(m.provider='openai',oa.revision,r.revision),u.relevance),
		v.title,v.description_md,v.salary_from,v.salary_to,COALESCE(p.resume_md,''),COALESCE(sp.markdown,''),
		COALESCE(m.provider,''),COALESCE(m.openai_model,''),COALESCE(m.openrouter_model,''),COALESCE(m.reasoning_effort,''),COALESCE(m.revision,0)
		FROM company_vacancies v JOIN app_runtime_generation g ON g.id=1
		LEFT JOIN user_profiles p ON p.user_name=?
		LEFT JOIN system_prompts sp ON sp.kind='relevance'
		LEFT JOIN prompt_model_settings m ON m.kind='relevance'
		LEFT JOIN openrouter_settings r ON r.id=1 LEFT JOIN openai_settings oa ON oa.id=1
		LEFT JOIN company_vacancy_user_state u ON u.user_name=? AND u.vacancy_id=v.id
		WHERE v.id=?`
	if _, locked := q.(*sql.Tx); locked {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, user, user, id).Scan(&v.generation, &v.snapshot, &v.title, &v.body, &v.from, &v.to, &v.resume, &v.prompt,
		&v.config.Provider, &v.config.OpenAIModel, &v.config.OpenRouterModel, &v.config.ReasoningEffort, &v.config.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrManualRelevanceMissing
	}
	return v, err
}

func (s ManualRelevanceService) Recalculate(ctx context.Context, user string, id int64) (ManualRelevanceResult, error) {
	if s.DB == nil || s.Generator == nil || user == "" || id < 1 {
		return ManualRelevanceResult{}, ErrJobInput
	}
	input, err := readManualRelevance(ctx, s.DB, user, id)
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	req, err := prepareRelevanceRequest(input.prompt, input.resume, input.title, input.body, nullableInt(input.from), nullableInt(input.to))
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	if profiles.ValidateModel("relevance", input.config) != nil {
		return ManualRelevanceResult{}, ErrJobInput
	}
	data, err := json.Marshal(req.State)
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	req.Messages = []aiproviders.Message{
		{Role: "system", Content: req.Instructions + "\nВерни только JSON с целым полем relevance от 0 до 100."},
		{Role: "user", Content: string(data)},
	}
	req.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
	if input.config.Provider == "openai" {
		req.ResponseFormat = json.RawMessage(`{"type":"json_schema","json_schema":{"name":"vacancy_relevance","strict":true,"schema":{"type":"object","properties":{"relevance":{"type":"integer"}},"required":["relevance"],"additionalProperties":false}}}`)
	}
	check := func(ctx context.Context, q relevanceQuery) error {
		current, err := readManualRelevance(ctx, q, user, id)
		if err != nil || !reflect.DeepEqual(current, input) {
			return ErrRelevanceChanged
		}
		return nil
	}
	req.CheckActive = func(ctx context.Context) error { return check(ctx, s.DB) }
	fingerprint, err := canonicalHash([]any{input.generation, user, id, input.snapshot})
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	billing := &manualRelevanceBilling{db: s.DB, check: check, fingerprint: "inflight:" + fingerprint}
	out, err := s.Generator.Automatic(ctx, req, input.config, billing)
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	score, valid := parseRelevanceAnswer(out.Message.Content)
	if !valid || billing.receipt == 0 {
		return ManualRelevanceResult{}, ErrSnapshotInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ManualRelevanceResult{}, err
	}
	defer tx.Rollback()
	if err = lockManualRelevance(ctx, tx, check); err != nil {
		return ManualRelevanceResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at)
		VALUES(?,?,?,CURRENT_TIMESTAMP) ON DUPLICATE KEY UPDATE relevance=VALUES(relevance),updated_at=VALUES(updated_at)`, user, id, score); err != nil {
		return ManualRelevanceResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE openrouter_ai_logs SET success=1,actual_model=?,error_code=NULL WHERE id=? AND task='manual_relevance' AND error_code=?", out.Model, billing.receipt, billing.fingerprint); err != nil {
		return ManualRelevanceResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ManualRelevanceResult{}, err
	}
	return ManualRelevanceResult{id, score, out.Model}, nil
}

func lockManualRelevance(ctx context.Context, tx *sql.Tx, check func(context.Context, relevanceQuery) error) error {
	var generation string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return err
	}
	return check(ctx, tx)
}

// Запись перед вызовом провайдера защищает от повторного запроса с теми же
// данными в течение двух минут, в том числе после обрыва соединения.
type manualRelevanceBilling struct {
	db          *sql.DB
	check       func(context.Context, relevanceQuery) error
	fingerprint string
	receipt     int64
}

func (b *manualRelevanceBilling) Run(ctx context.Context, request json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	var meta struct {
		Model string `json:"model"`
		Free  bool   `json:"free"`
	}
	if work == nil || json.Unmarshal(request, &meta) != nil || meta.Model == "" || len(meta.Model) > 200 {
		return nil, ErrJobInput
	}
	if b.receipt != 0 {
		if !meta.Free {
			return nil, ErrManualRelevanceBusy
		}
		return work(ctx)
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockManualRelevance(ctx, tx, b.check); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM openrouter_ai_logs WHERE task='manual_relevance' AND error_code=? AND created_at >= UTC_TIMESTAMP() - INTERVAL 2 MINUTE)", b.fingerprint).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrManualRelevanceBusy
	}
	row, err := tx.ExecContext(ctx, "INSERT INTO openrouter_ai_logs(created_at,task,requested_primary_model,success,error_code) VALUES(UTC_TIMESTAMP(),'manual_relevance',?,0,?)", meta.Model, b.fingerprint)
	if err != nil {
		return nil, err
	}
	b.receipt, err = row.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return work(ctx)
}
