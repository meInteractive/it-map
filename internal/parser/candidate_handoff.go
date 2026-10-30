package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/backup"
	"reflect"
	"strings"
)

// Next должен повторно вычислять зависимости и scope из DB в текущей owned TX.
// Не используйте payload задания или данные HTTP для принятия решения.
type CandidateHandoffPlanner interface {
	Next(context.Context, *sql.Tx, Job) (*CandidateStagePlan, error)
}
type CandidateHandoff struct{ Planner CandidateHandoffPlanner }

// Enqueue вызывается только из FinishWithin после сохранения результата стадии.
func (h *CandidateHandoff) Enqueue(ctx context.Context, tx *sql.Tx, job Job) error {
	if h == nil || h.Planner == nil || tx == nil || job.Generation == "" || job.CandidateID == nil || job.SourceID == nil || (job.CompanyID == nil) == (job.AgencyID == nil) {
		return ErrJobInput
	}
	next, e := h.Planner.Next(ctx, tx, job)
	if e != nil {
		return e
	}
	if next == nil {
		return nil
	}
	switch next.Type {
	case "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy":
	default:
		return ErrJobInput
	}
	if next.CandidateID != *job.CandidateID || !reflect.DeepEqual(next.CompanyID, job.CompanyID) || !reflect.DeepEqual(next.AgencyID, job.AgencyID) || next.SourceID != *job.SourceID || !strings.HasPrefix(next.Key, fmt.Sprintf("%s:%d:", next.Type, next.CandidateID)) || utf16Length(next.Key) > 500 {
		return ErrJobInput
	}
	row, e := readSettingsRow(ctx, tx, true)
	if e != nil {
		return ErrSettingsStorage
	}
	view, e := settingsView(ctx, tx, row)
	if e != nil {
		return ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled {
		return ErrSettingsChanged
	}
	priority, ok := settings.Priorities[next.Type]
	if !ok || priority < 0 || priority > 100 {
		return ErrSettingsStorage
	}
	payload, e := json.Marshal(next.Payload)
	if e != nil {
		return ErrJobInput
	}
	if next.Payload == nil {
		payload = []byte(`{}`)
	}
	payload, e = backup.StringifyParsedJSON(payload)
	if e != nil {
		return ErrJobInput
	}
	id, e := stageToken()
	if e != nil {
		return e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	dedupe := stageHash(next.Key)
	_, e = tx.ExecContext(ctx, `INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,4,?,?) ON DUPLICATE KEY UPDATE id=id`, id, job.Generation, dedupe, next.Type, next.CompanyID, next.AgencyID, next.CandidateID, next.SourceID, string(payload), priority, now, now, now)
	if e != nil {
		return ErrSettingsStorage
	}
	var kind, source string
	var company, agency, candidate sql.NullInt64
	var storedPayload []byte
	e = tx.QueryRowContext(ctx, `SELECT type,company_id,agency_id,candidate_id,source_id,payload FROM scheduler_jobs WHERE generation=? AND dedupe_key=? FOR UPDATE`, job.Generation, dedupe).Scan(&kind, &company, &agency, &candidate, &source, &storedPayload)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrSettingsStorage
	}
	if e != nil {
		return ErrSettingsStorage
	}
	storedPayload, e = backup.StringifyParsedJSON(storedPayload)
	if e != nil {
		return ErrSettingsStorage
	}
	if kind != next.Type || !reflect.DeepEqual(nullableInt(company), next.CompanyID) || !reflect.DeepEqual(nullableInt(agency), next.AgencyID) || !candidate.Valid || candidate.Int64 != next.CandidateID || source != next.SourceID || string(storedPayload) != string(payload) {
		return ErrSettingsChanged
	}
	return nil
}
