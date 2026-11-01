//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
	"testing"
)

type hhDatasetProvider struct {
	*runtimeProvider
	infos   int
	changed bool
	reads   int
}

func (p *hhDatasetProvider) DatasetInfo(context.Context, string) (apify.DatasetMetadata, error) {
	p.infos++
	count := int64(1)
	if p.changed && p.infos%2 == 0 {
		count = 2
	}
	return apify.DatasetMetadata{ItemCount: count, ModifiedAt: "2026-11-01T00:00:00Z"}, nil
}
func (p *hhDatasetProvider) DatasetItems(_ context.Context, id string, offset int64, clean bool) ([]json.RawMessage, error) {
	p.reads++
	if id != "CIData123" || clean {
		return nil, errors.New("wrong dataset scope/mode")
	}
	return []json.RawMessage{json.RawMessage(`{"title":"CI title","url":"https://hh.ru/vacancy/123","employer_id":456}`)}, nil
}
func TestMariaDBHHListingDatasetDisposable(t *testing.T) {
	f := openApifyRuntimeFixture(t)
	p := &runtimeProvider{}
	s := runtimeService(f, p)
	if _, e := s.Execute(f.ctx, f.v, "CI dataset", apify.HHOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute(f.ctx, f.v, "CI dataset", apify.HHOptions{}); e != nil {
		t.Fatal(e)
	}
	dataset := &hhDatasetProvider{runtimeProvider: p}
	s.NewClient = func(string) (apifyruns.Provider, error) { return dataset, nil }
	got, e := s.ReadDataset(f.ctx, f.v, "CI dataset", apify.HHOptions{}, 0)
	if e != nil || len(got.Items) != 1 || got.Metadata.ItemCount != 1 || got.Record.DatasetID != "CIData123" || dataset.reads != 1 || p.starts.Load() != 1 {
		t.Fatal("dataset read failed or started another actor", e)
	}
	dataset.changed = true
	dataset.infos = 0
	if _, e := s.ReadDataset(f.ctx, f.v, "CI dataset", apify.HHOptions{}, 0); e == nil {
		t.Fatal("changing dataset accepted")
	}
	reads := dataset.reads
	if _, e := s.ReadDataset(f.ctx, f.v, "CI dataset", apify.HHOptions{}, -1); e == nil || dataset.reads != reads {
		t.Fatal("invalid offset sent request")
	}
}
