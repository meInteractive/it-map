package apifyruns

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

var ErrDatasetChanged = errors.New("dataset Apify изменился во время чтения")

type DatasetProvider interface {
	DatasetInfo(context.Context, string) (apify.DatasetMetadata, error)
	DatasetItems(context.Context, string, int64, bool) ([]json.RawMessage, error)
}
type DatasetSlice struct {
	Record   Record
	Metadata apify.DatasetMetadata
	Items    []json.RawMessage
}

// ReadDataset читает только известный terminal run, всегда в raw-режиме.
// Проверки до и после чтения запрещают продолжение по смешанному снимку.
func (s Service) ReadDataset(ctx context.Context, v apifysettings.OperationVersion, key string, options apify.HHOptions, offset int64) (DatasetSlice, error) {
	scoped, e := operationKey(v, key, options)
	if e != nil {
		return DatasetSlice{}, e
	}
	return s.readDataset(ctx, v, scoped, offset, "", "")
}

// ReadPreviousDataset читает только уже завершённый оплаченный запуск. Текущая
// версия разрешений и ключ проверяются как обычно; старый ключ операции нужен
// исключительно для поиска прежней записи, право на новый POST не выдаётся.
func (s Service) ReadPreviousDataset(ctx context.Context, previous, current apifysettings.OperationVersion, key string, options apify.HHOptions, offset int64, runID, datasetID string) (DatasetSlice, error) {
	if err := validatePreviousDataset(previous, current, options, runID, datasetID); err != nil {
		return DatasetSlice{}, apifysettings.ErrChanged
	}
	scoped, e := operationKey(previous, key, options)
	if e != nil {
		return DatasetSlice{}, e
	}
	return s.readDataset(ctx, current, scoped, offset, runID, datasetID)
}

func validatePreviousDataset(previous, current apifysettings.OperationVersion, options apify.HHOptions, runID, datasetID string) error {
	if previous.Generation != current.Generation || previous.CompanyID != current.CompanyID || previous.SourceURL != current.SourceURL || previous.Profile != current.Profile || previous.CandidateID != 0 || current.CandidateID != 0 || previous.TitleRevision != 0 || current.TitleRevision != 0 || previous.VacancyURL != "" || current.VacancyURL != "" || options.Detail || previous.SourceRevision > current.SourceRevision || previous.SettingsRevision > current.SettingsRevision || runID == "" || datasetID == "" {
		return apifysettings.ErrChanged
	}
	return nil
}

// VerifyPreviousDatasetTx проверяет прежнюю денежную запись в транзакции
// обновления курсора. Никакие сетевые запросы и расходы здесь невозможны.
func (s Service) VerifyPreviousDatasetTx(ctx context.Context, tx *sql.Tx, previous, current apifysettings.OperationVersion, key string, options apify.HHOptions, runID, datasetID string) error {
	if tx == nil || s.Repository.DB == nil || s.Repository.Settings.DB != s.Repository.DB {
		return ErrStorage
	}
	if err := validatePreviousDataset(previous, current, options, runID, datasetID); err != nil {
		return err
	}
	scoped, err := operationKey(previous, key, options)
	if err != nil {
		return err
	}
	token, err := s.Repository.Settings.ResolveOperationTx(ctx, tx, current)
	if err != nil {
		return err
	}
	var state, storedRun, storedDataset, storedHash, profile string
	var charge sql.NullFloat64
	err = tx.QueryRowContext(ctx, "SELECT state,run_id,dataset_id,key_hash,profile,charged_usd FROM apify_runs WHERE generation=? AND operation_key=? FOR UPDATE", current.Generation, scoped).Scan(&state, &storedRun, &storedDataset, &storedHash, &profile, &charge)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUncertain
	}
	if err != nil {
		return ErrStorage
	}
	hash := sha256.Sum256([]byte(token))
	if state != "succeeded" || !charge.Valid || storedRun != runID || storedDataset != datasetID || profile != current.Profile || storedHash != hex.EncodeToString(hash[:]) {
		return ErrDatasetChanged
	}
	return nil
}

func (s Service) readDataset(ctx context.Context, v apifysettings.OperationVersion, scoped string, offset int64, expectedRun, expectedDataset string) (DatasetSlice, error) {
	if offset < 0 || offset > 9007199254740991 {
		return DatasetSlice{}, ErrDatasetChanged
	}
	if err := s.checkActive(ctx); err != nil {
		return DatasetSlice{}, err
	}
	row, e := s.Repository.readOperation(ctx, v, scoped)
	if e != nil {
		return DatasetSlice{}, e
	}
	if row.RunID == "" || row.DatasetID == "" || row.State != "succeeded" && row.State != "failed" {
		return DatasetSlice{}, ErrUncertain
	}
	if expectedRun != "" && (row.State != "succeeded" || row.ChargedUSD == nil || row.RunID != expectedRun || row.DatasetID != expectedDataset) {
		return DatasetSlice{}, ErrDatasetChanged
	}
	token, e := s.Repository.Settings.ResolveOperation(ctx, v)
	if e != nil {
		return DatasetSlice{}, e
	}
	client, e := s.client(token)
	if e != nil {
		return DatasetSlice{}, e
	}
	dataset, ok := client.(DatasetProvider)
	if !ok {
		return DatasetSlice{}, ErrStorage
	}
	if err := s.checkActive(ctx); err != nil {
		return DatasetSlice{}, err
	}
	before, e := dataset.DatasetInfo(ctx, row.DatasetID)
	if e != nil {
		return DatasetSlice{}, e
	}
	if err := s.checkActive(ctx); err != nil {
		return DatasetSlice{}, err
	}
	items, e := dataset.DatasetItems(ctx, row.DatasetID, offset, false)
	if e != nil {
		return DatasetSlice{}, e
	}
	if err := s.checkActive(ctx); err != nil {
		return DatasetSlice{}, err
	}
	after, e := dataset.DatasetInfo(ctx, row.DatasetID)
	if e != nil {
		return DatasetSlice{}, e
	}
	if before.ItemCount != after.ItemCount || before.ModifiedAt != after.ModifiedAt || before.ItemCount < 0 || before.ItemCount > 9007199254740991 || len(items) > 10 || offset > before.ItemCount || int64(len(items)) > before.ItemCount-offset {
		return DatasetSlice{}, ErrDatasetChanged
	}
	if len(items) < 10 && offset+int64(len(items)) != before.ItemCount {
		return DatasetSlice{}, ErrDatasetChanged
	}
	if _, e = s.Repository.Settings.ResolveOperation(ctx, v); e != nil {
		return DatasetSlice{}, e
	}
	if err := s.checkActive(ctx); err != nil {
		return DatasetSlice{}, err
	}
	return DatasetSlice{Record: row, Metadata: after, Items: items}, nil
}
