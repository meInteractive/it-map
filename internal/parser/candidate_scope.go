package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// readCandidateScope проверяет источник полномочий в той же транзакции, что захват задания.
func readCandidateScope(ctx context.Context, tx *sql.Tx, generation string, scope *ScanCandidateScope) (map[int64]bool, error) {
	if scope == nil || !generationPattern.MatchString(scope.RequestID) || !generationPattern.MatchString(scope.ScanJobID) || scope.CompanyID < 1 || scope.CompanyID > maxSafeInteger {
		return nil, ErrJobInput
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return nil, ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return nil, ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName == "" {
		return nil, ErrSettingsChanged
	}
	selected := false
	for _, id := range settings.SourceIDs {
		if id == "hh" {
			selected = true
		}
	}
	if !selected {
		return nil, ErrSettingsChanged
	}
	var found int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_jobs j JOIN scheduler_attempts a ON a.job_id=j.id AND a.run_id=? JOIN vacancy_source_parsing s ON s.company_id=j.company_id AND s.source_id='hh' AND s.enabled=1 WHERE j.id=? AND j.generation=? AND j.dedupe_key=? AND j.type='scan_source' AND j.status IN ('pending','completed') AND j.company_id=? AND j.agency_id IS NULL AND j.source_id='hh'`, runID(generation, scope.RequestID), scope.ScanJobID, generation, stageHash("target-hh:"+scope.RequestID), scope.CompanyID).Scan(&found)
	if err != nil {
		return nil, ErrSettingsStorage
	}
	if found != 1 {
		return nil, ErrHHListingChanged
	}
	source := "hh"
	company := scope.CompanyID
	job := Job{ID: scope.ScanJobID, Generation: generation, CompanyID: &company, SourceID: &source}
	cursor, exists, err := readHHListingCursor(ctx, tx, "go399:"+scope.ScanJobID, settings.OwnerName, job)
	if err != nil {
		return nil, err
	}
	if !exists || cursor.LineageVersion != 1 {
		return nil, ErrHHListingChanged
	}
	var name, sourceURL string
	var sourceRevision int64
	err = tx.QueryRowContext(ctx, `SELECT c.company_name,s.source_url,s.revision FROM company_identity c JOIN vacancy_source_parsing s ON s.company_id=c.id AND s.source_id='hh' AND s.enabled=1 WHERE c.id=? FOR UPDATE`, scope.CompanyID).Scan(&name, &sourceURL, &sourceRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHHListingChanged
	}
	if err != nil {
		return nil, ErrSettingsStorage
	}
	if sourceURL != cursor.Operation.SourceURL || sourceRevision != cursor.Operation.SourceRevision {
		return nil, ErrHHListingChanged
	}
	if row.favorite != 0 {
		var favorite int
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM company_user_state WHERE company_name=? AND user_name=? AND favorite=1", name, settings.OwnerName).Scan(&favorite)
		if err != nil {
			return nil, ErrSettingsStorage
		}
		if favorite != 1 {
			return nil, ErrHHListingChanged
		}
	}
	out := make(map[int64]bool, len(cursor.CandidateIDs))
	for _, id := range cursor.CandidateIDs {
		out[id] = true
	}
	return out, nil
}

func inCandidateScope(job Job, scope *ScanCandidateScope, ids map[int64]bool) bool {
	if scope == nil {
		return true
	}
	switch job.Type {
	case "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance":
	default:
		return false
	}
	return job.CompanyID != nil && *job.CompanyID == scope.CompanyID && job.AgencyID == nil && job.SourceID != nil && *job.SourceID == "hh" && job.CandidateID != nil && ids[*job.CandidateID]
}
