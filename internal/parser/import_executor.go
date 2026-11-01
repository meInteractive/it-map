package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"itmap/internal/apifysettings"
)

var ErrImportReceiptConflict = errors.New("vacancy import receipt conflict")

type HHImportResult struct {
	CandidateID, VacancyID int64
	Action                 string
	Reused                 bool
}
type HHImportExecutor struct {
	Settings apifysettings.Repository
	Owner    string
}
type importReceipt struct{ Generation, DedupeKey, LeafInputHash, SnapshotID, ExtractionID, RoleResultID string }

// Execute сохраняет результат только внутри принадлежащей заданию транзакции.
func (e HHImportExecutor) Execute(ctx context.Context, lease *JobLease) (result HHImportResult, err error) {
	if lease == nil || e.Settings.DB == nil || e.Settings.DB != lease.repo.DB || e.Owner == "" {
		return result, ErrJobInput
	}
	job := lease.Job()
	if job.Type != "import_vacancy" || job.CandidateID == nil || job.CompanyID == nil || job.AgencyID != nil || job.SourceID == nil || *job.SourceID != "hh" {
		return result, ErrJobInput
	}
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		token, err := acquireImportGlobal(ctx, tx, job)
		if err != nil {
			return err
		}
		source, err := e.Settings.ReadHHSourceTx(ctx, tx, *job.CompanyID)
		if err != nil {
			return err
		}
		if !source.Enabled || !source.Supported {
			return apifysettings.ErrDisabled
		}
		var url, canonical, sourceURL, company string
		var namespace, external sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT vacancy_url,canonical_url,source_url,source_namespace,external_job_id FROM vacancy_candidates WHERE id=? FOR UPDATE", job.CandidateID).Scan(&url, &canonical, &sourceURL, &namespace, &external); err != nil {
			return ErrCandidateChanged
		}
		if sourceURL != source.URL || canonical != url || importHHIdentity(url) == "" {
			return ErrCandidateChanged
		}
		if err = tx.QueryRowContext(ctx, "SELECT company_name FROM company_identity WHERE id=? FOR UPDATE", job.CompanyID).Scan(&company); err != nil || company == "" {
			return ErrCandidateChanged
		}
		var receiptID, receiptStatus, receiptAction, receiptData, receiptHash string
		var receiptCandidate, receiptVacancy sql.NullInt64
		err = tx.QueryRowContext(ctx, "SELECT id,candidate_id,vacancy_id,status,action,changes_json,COALESCE(input_hash,'') FROM vacancy_import_operations WHERE id=? FOR UPDATE", job.ID).Scan(&receiptID, &receiptCandidate, &receiptVacancy, &receiptStatus, &receiptAction, &receiptData, &receiptHash)
		exists := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ErrSettingsStorage
		}
		var receipt importReceipt
		if exists && (receiptStatus != "success" || !receiptCandidate.Valid || receiptCandidate.Int64 != *job.CandidateID || !receiptVacancy.Valid || receiptVacancy.Int64 <= 0 || json.Unmarshal([]byte(receiptData), &receipt) != nil || receipt.Generation != job.Generation || receipt.DedupeKey != job.DedupeKey || receipt.LeafInputHash != receiptHash) {
			return ErrImportReceiptConflict
		}
		snapshot, leaf, err := readImportStageSnapshot(ctx, tx, job, e.Owner, exists)
		if err != nil {
			return err
		}
		if exists {
			if receipt.LeafInputHash != snapshot.InputHash || receipt.SnapshotID != snapshot.Extraction.Snapshot.ID || receipt.ExtractionID != snapshot.ExtractCache.ID || receipt.RoleResultID != leaf.ID {
				return ErrImportReceiptConflict
			}
			var linked sql.NullInt64
			if tx.QueryRowContext(ctx, "SELECT main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE", job.CandidateID).Scan(&linked) != nil || !linked.Valid || linked.Int64 != receiptVacancy.Int64 {
				return ErrImportReceiptConflict
			}
			var count int
			if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vacancy_publication_sources WHERE candidate_id=? AND vacancy_id=? AND extraction_id=?", job.CandidateID, receiptVacancy.Int64, receipt.ExtractionID).Scan(&count) != nil || count != 1 {
				return ErrImportReceiptConflict
			}
			result = HHImportResult{*job.CandidateID, receiptVacancy.Int64, receiptAction, true}
		} else {
			result, err = writeHHImport(ctx, tx, job, company, url, canonical, namespace, external, snapshot, leaf)
			if err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_detail_leases WHERE lease_key='import:global' AND generation=? AND owner_token=?", job.Generation, token); err != nil {
			return ErrSettingsStorage
		}
		return nil
	})
	if err != nil {
		return HHImportResult{}, err
	}
	return result, nil
}

func acquireImportGlobal(ctx context.Context, tx *sql.Tx, job Job) (string, error) {
	token, err := stageToken()
	if err != nil {
		return "", err
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return "", ErrSettingsStorage
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES('import:global',?,?,?) ON DUPLICATE KEY UPDATE owner_token=IF(expires_at<=?,VALUES(owner_token),owner_token),generation=IF(owner_token=VALUES(owner_token),VALUES(generation),generation),expires_at=IF(owner_token=VALUES(owner_token),VALUES(expires_at),expires_at)`, job.Generation, token, now+int64((150*time.Second)/time.Millisecond), now)
	if err != nil {
		return "", ErrSettingsStorage
	}
	var actual string
	if tx.QueryRowContext(ctx, "SELECT owner_token FROM vacancy_detail_leases WHERE lease_key='import:global' FOR UPDATE").Scan(&actual) != nil {
		return "", ErrSettingsStorage
	}
	if actual != token {
		return "", ErrDetailBusy
	}
	return token, nil
}
