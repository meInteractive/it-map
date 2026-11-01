package apifyruns

import (
	"context"
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
	if offset < 0 || offset > 9007199254740991 {
		return DatasetSlice{}, ErrDatasetChanged
	}
	scoped, e := operationKey(v, key, options)
	if e != nil {
		return DatasetSlice{}, e
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
