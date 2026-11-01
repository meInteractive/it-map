package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"itmap/internal/apifysettings"
)

type hhFullTextPlan struct {
	Title                                                        CandidateTitleInput
	Source                                                       apifysettings.HHSource
	URL, SchedulerHash, LastSnapshotID, LastCreated, Token, Slot string
	Started                                                      int64
	Cached, Previous                                             *StoredRawSnapshot
}

func (e HHFullTextExecutor) readHHFullTextPlan(ctx context.Context, tx *sql.Tx, j Job) (hhFullTextPlan, error) {
	var plan hhFullTextPlan
	if j.Type != "fetch_full_text" || j.CandidateID == nil || j.CompanyID == nil || j.AgencyID != nil || j.SourceID == nil || *j.SourceID != "hh" {
		return plan, ErrJobInput
	}
	title, err := ReadCandidateTitleInput(ctx, tx, j, e.Owner)
	if err != nil {
		return plan, err
	}
	matched := false
	for _, r := range title.Roles {
		if r.Decision == nil {
			return plan, ErrCandidateChanged
		}
		matched = matched || *r.Decision == "match"
	}
	if !matched {
		return plan, ErrCandidateChanged
	}
	var main sql.NullInt64
	var updated sql.NullString
	if tx.QueryRowContext(ctx, "SELECT vacancy_url,title_updated_at,main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE", j.CandidateID).Scan(&plan.URL, &updated, &main) != nil {
		return plan, ErrSettingsStorage
	}
	if main.Valid || !sameHHDetailURL(plan.URL, plan.URL) {
		return plan, ErrCandidateChanged
	}
	plan.Source, err = e.Settings.ReadHHSourceTx(ctx, tx, *j.CompanyID)
	if err != nil {
		return plan, err
	}
	if !plan.Source.Enabled || !plan.Source.Supported {
		return plan, apifysettings.ErrDisabled
	}
	settings, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return plan, ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, settings)
	if err != nil {
		return plan, ErrSettingsStorage
	}
	var current Settings
	if json.Unmarshal(view.PublicJSON(), &current) != nil || !current.Enabled || current.OwnerName != e.Owner {
		return plan, ErrCandidateChanged
	}
	plan.SchedulerHash = stageHash(string(view.PublicJSON()))
	plan.Title = title
	err = tx.QueryRowContext(ctx, "SELECT id,created_at FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE", j.CandidateID, plan.URL).Scan(&plan.LastSnapshotID, &plan.LastCreated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return plan, ErrSettingsStorage
	}
	cached, err := ReadRawSnapshot(ctx, tx, *j.CandidateID, plan.URL, "hh")
	if errors.Is(err, ErrSnapshotMissing) || errors.Is(err, ErrSnapshotInvalid) || errors.Is(err, ErrSnapshotAmbiguous) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	if !sameHHDetailURL(cached.FinalURL, plan.URL) || len(CheckFulltextContent(cached.Markdown, 200)) != 0 {
		return plan, nil
	}
	plan.Previous = &cached
	input := CandidateStageInput{TitleUpdatedAt: updated.String, Snapshot: &CandidateStageSnapshot{ID: cached.ID, FetchedAt: cached.FetchedAt}}
	var fetched int64
	err = tx.QueryRowContext(ctx, "SELECT started_at FROM vacancy_content_fetches WHERE candidate_id=? AND status='success' ORDER BY started_at DESC LIMIT 1 FOR UPDATE", j.CandidateID).Scan(&fetched)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return plan, ErrSettingsStorage
	}
	if err == nil {
		input.LastSuccessfulFetch = &fetched
	}
	refresh, err := candidateTextRefresh(input)
	if err != nil {
		return plan, err
	}
	if !refresh {
		plan.Cached = &cached
	}
	return plan, nil
}
func (e HHFullTextExecutor) validateHHFullTextPlan(ctx context.Context, tx *sql.Tx, j Job, expected hhFullTextPlan) error {
	current, err := e.readHHFullTextPlan(ctx, tx, j)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current.Title, expected.Title) || current.Source.URL != expected.Source.URL || current.Source.Revision != expected.Source.Revision || current.URL != expected.URL || current.SchedulerHash != expected.SchedulerHash || current.LastSnapshotID != expected.LastSnapshotID {
		return ErrCandidateChanged
	}
	if expected.Token != "" {
		return validateDetailLeases(ctx, tx, j, *j.CandidateID, expected.Slot, expected.Token, true)
	}
	return nil
}
func saveHHFetchHistory(ctx context.Context, tx *sql.Tx, j Job, plan hhFullTextPlan, snapshotID, provider, profile string, valid bool) error {
	id, err := stageToken()
	if err != nil {
		return err
	}
	var snapshot any
	if snapshotID != "" {
		snapshot = snapshotID
	}
	status := "success"
	var code any
	message := ""
	if !valid {
		status = "error"
		code = "hh_content_invalid"
		message = "HH не вернул проверенное полное описание вакансии."
	}
	now := time.Now().UnixMilli()
	mode := provider
	if provider == "apify" {
		mode += "-" + profile
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO vacancy_content_fetches(id,candidate_id,snapshot_id,source_url,final_url,firecrawl_mode,started_at,duration_ms,status,error_code,message) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, j.CandidateID, snapshot, plan.URL, plan.URL, mode, plan.Started, max(int64(0), now-plan.Started), status, code, message)
	if err != nil {
		return ErrSettingsStorage
	}
	return nil
}
func saveHHFullTextSnapshot(ctx context.Context, tx *sql.Tx, j Job, plan hhFullTextPlan, document hhDetailDocument, provider, profile, runID string) (HHFullTextResult, error) {
	hash := stageHash(document.Markdown)
	result := HHFullTextResult{}
	if plan.Previous != nil && plan.Previous.ContentHash == hash {
		result.SnapshotID = plan.Previous.ID
		result.Reused = true
	} else {
		id, err := stageToken()
		if err != nil {
			return result, err
		}
		result.SnapshotID = id
		now := time.Now().UTC()
		if old, err := time.Parse(time.RFC3339Nano, plan.LastCreated); err == nil && !now.After(old) {
			now = old.Add(time.Millisecond)
		}
		timestamp := now.Format("2006-01-02T15:04:05.000Z")
		// Миллисекундная точность SQL не должна создавать два «последних» снимка.
		if timestamp <= plan.LastCreated {
			if old, err := time.Parse(time.RFC3339Nano, plan.LastCreated); err == nil {
				timestamp = old.Add(time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z")
			}
		}
		diagnostics, _ := json.Marshal(map[string]any{"errors": []string{}, "valid": true, "provider": provider, "profile": profile, "runId": runID, "descriptionFormat": document.Format})
		var inline any = document.Markdown
		large := len(document.Markdown) > 180000
		if large {
			inline = nil
		}
		mode := "free"
		if profile == "starter" {
			mode = "hobby"
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES(?,?,?,?,?,?,?,?,'valid',?,?)", id, j.CandidateID, plan.URL, document.URL, inline, hash, timestamp, mode, string(diagnostics), timestamp); err != nil {
			return result, ErrSettingsStorage
		}
		if large {
			part := 0
			start := 0
			for at := range document.Markdown {
				if at-start >= 48000 {
					if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw) VALUES(?,?,?)", id, part, document.Markdown[start:at]); err != nil {
						return result, ErrSettingsStorage
					}
					part++
					start = at
				}
			}
			if start < len(document.Markdown) {
				if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw) VALUES(?,?,?)", id, part, document.Markdown[start:]); err != nil {
					return result, ErrSettingsStorage
				}
			}
		}
	}
	if err := saveHHFetchHistory(ctx, tx, j, plan, result.SnapshotID, provider, profile, true); err != nil {
		return HHFullTextResult{}, err
	}
	return result, nil
}
