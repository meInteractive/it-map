package parser

import (
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
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
	c := hhListingCursor{Version: 1, Profile: "free", MoscowFilter: true, Page: 0, Offset: 100, PageCount: 100, RunID: "CIRun", DatasetID: "CIData", Metadata: &slice.Metadata, PreviousOperation: &apifysettings.OperationVersion{CompanyID: 1}}
	next, e := advanceHHListingCursor(c, slice, 0, "", now)
	if e != nil || next.Page != 1 || next.Offset != 0 || next.Done || next.RunID != "" || next.PreviousOperation != nil {
		t.Fatal("next HH page", next, e)
	}
	legacy := c
	legacy.MoscowFilter = false
	stopped, err := advanceHHListingCursor(legacy, slice, 0, "", now)
	if err != nil || !stopped.Done || stopped.Page != 0 || stopped.PartialCode != "moscow_filter_upgrade" {
		t.Fatal("legacy page could start a new unfiltered Actor", stopped, err)
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

func TestHHScanCandidateLineage(t *testing.T) {
	legacy := hhListingCursor{}
	if err := legacy.rememberCandidate(1); err != nil || legacy.LineageVersion != 0 || len(legacy.CandidateIDs) != 0 {
		t.Fatal("legacy cursor must remain unverified", err)
	}
	current := hhListingCursor{LineageVersion: 1}
	for _, id := range []int64{11, 12, 11} {
		if err := current.rememberCandidate(id); err != nil {
			t.Fatal(err)
		}
	}
	if len(current.CandidateIDs) != 2 || current.CandidateIDs[0] != 11 || current.CandidateIDs[1] != 12 {
		t.Fatal("lineage is not unique", current.CandidateIDs)
	}
	if err := current.rememberCandidate(0); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	full := hhListingCursor{LineageVersion: 1, CandidateIDs: make([]int64, 10000)}
	if err := full.rememberCandidate(1); err == nil {
		t.Fatal("oversized lineage accepted")
	}
	if err := (&hhListingCursor{LineageVersion: 2}).rememberCandidate(1); err == nil {
		t.Fatal("unknown lineage version accepted")
	}
	for _, tc := range []struct {
		c     hhListingCursor
		valid bool
	}{
		{hhListingCursor{}, true},
		{hhListingCursor{LineageVersion: 1, CandidateIDs: []int64{1, 2}}, true},
		{hhListingCursor{CandidateIDs: []int64{1}}, false},
		{hhListingCursor{LineageVersion: 1, CandidateIDs: []int64{1, 1}}, false},
		{hhListingCursor{LineageVersion: 1, CandidateIDs: []int64{0}}, false},
		{hhListingCursor{LineageVersion: 2}, false},
	} {
		if tc.c.validLineage() != tc.valid {
			t.Fatal("invalid stored lineage validation", tc)
		}
	}
}
