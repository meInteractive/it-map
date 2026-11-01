package parser

import (
	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
	"testing"
	"time"
)

func TestHHListingCursorAdvancesOnlyStableFixedDataset(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	cost := 0.02
	c := hhListingCursor{Version: 1, Profile: "free"}
	slice := apifyruns.DatasetSlice{Record: apifyruns.Record{State: "succeeded", RunID: "CIRun", DatasetID: "CIData", ChargedUSD: &cost}, Metadata: apify.DatasetMetadata{ItemCount: 11, ModifiedAt: "2026-11-01T11:59:00Z"}}
	c, e := advanceHHListingCursor(c, slice, 10, "prefix-a", now)
	if e != nil || c.Offset != 10 || c.Done {
		t.Fatal("first raw slice", c, e)
	}
	c, e = advanceHHListingCursor(c, slice, 1, "", now)
	if e != nil || c.Offset != 11 || !c.Done || c.PartialCode != "" {
		t.Fatal("stable dataset did not complete", c, e)
	}
	changed := slice
	changed.Metadata.ItemCount = 12
	if _, e = advanceHHListingCursor(c, changed, 0, "", now); e == nil {
		t.Fatal("changed dataset silently resumed")
	}
}
func TestHHListingCursorHonorsCostFailureAndPageLimit(t *testing.T) {
	now := time.Now().UnixMilli()
	cost := 0.02
	slice := apifyruns.DatasetSlice{Record: apifyruns.Record{State: "succeeded", RunID: "CIRun", DatasetID: "CIData", ChargedUSD: &cost}, Metadata: apify.DatasetMetadata{ItemCount: 100, ModifiedAt: "2020-01-01T00:00:00Z"}}
	c := hhListingCursor{Version: 1, Profile: "free", Page: 0, Offset: 100, PageCount: 100, RunID: "CIRun", DatasetID: "CIData", Metadata: &slice.Metadata}
	next, e := advanceHHListingCursor(c, slice, 0, "", now)
	if e != nil || next.Page != 1 || next.Offset != 0 || next.Done || next.RunID != "" {
		t.Fatal("next HH page", next, e)
	}
	slice.Record.State = "failed"
	next, e = advanceHHListingCursor(c, slice, 0, "", now)
	if e != nil || !next.Done || next.PartialCode != "apify_partial_run" {
		t.Fatal("failed run restarted", next, e)
	}
	slice.Record.State = "succeeded"
	slice.Record.ChargedUSD = nil
	next, e = advanceHHListingCursor(c, slice, 0, "", now)
	if e != nil || next.Done || next.Page != 0 {
		t.Fatal("unknown charge authorized next page", next, e)
	}
}

func TestHHListingUnderfilledDatasetFailsWithoutAdvancing(t *testing.T) {
	cost := 0.02
	c := hhListingCursor{Version: 1, Profile: "free"}
	slice := apifyruns.DatasetSlice{Record: apifyruns.Record{State: "succeeded", RunID: "CIRun", DatasetID: "CIData", ChargedUSD: &cost}, Metadata: apify.DatasetMetadata{ItemCount: 11, ModifiedAt: "2020-01-01T00:00:00Z"}}
	for _, count := range []int{0, 1} {
		if _, err := advanceHHListingCursor(c, slice, count, "prefix", time.Now().UnixMilli()); err == nil {
			t.Fatal("incomplete short slice reported success", count)
		}
	}
}
