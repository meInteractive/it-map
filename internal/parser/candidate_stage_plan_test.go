package parser

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func checkCandidateStageSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Name, Source, TitleHash, PromptVersion, ExtractionConfigHash, ExtractionPromptVersion, LeafInputHash string
		Decision                                                                                             *string
		Snapshot, Refresh, Extraction, Disabled                                                              bool
		Leaf                                                                                                 []int64
		Roles                                                                                                []CandidateStageRole
		Result                                                                                               json.RawMessage
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 11 {
		t.Fatal("source corpus")
	}
	company := int64(990002)
	for _, f := range frames {
		t.Run(f.Name, func(t *testing.T) {
			in := CandidateStageInput{CandidateID: 990001, TitleRevision: 1, CompanyID: &company, SourceID: f.Source, TitleHash: f.TitleHash, SourceSelected: !f.Disabled, ScopeAllowed: true, Roles: f.Roles}
			if f.Snapshot {
				in.Snapshot = &CandidateStageSnapshot{ID: "CI_SNAPSHOT", FetchedAt: "2026-01-01T00:00:00.000Z"}
				in.ExtractionConfigHash = f.ExtractionConfigHash
				in.ExtractionPromptVersion = f.ExtractionPromptVersion
			}
			if f.Refresh {
				in.TitleUpdatedAt = "2026-01-02T00:00:00.000Z"
			}
			if f.Extraction {
				in.ExtractionID = "CI_EXTRACTION"
				in.LeafInputHash = f.LeafInputHash
			}
			if f.Leaf != nil {
				in.Leaf = &CandidateStageLeaf{ID: "CI_RESULT", InputHash: f.LeafInputHash, RoleIDs: f.Leaf}
			}
			got, e := PlanCandidateStage(in)
			if e != nil {
				t.Fatal(e)
			}
			bytes, _ := json.Marshal(got)
			var actual, want any
			_ = json.Unmarshal(bytes, &actual)
			_ = json.Unmarshal(f.Result, &want)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("source stage differs: %s expected %s", bytes, f.Result)
			}
		})
	}
}
func TestCandidateStageSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/candidate-stage-plan-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkCandidateStageSource(t, b)
}
func TestCandidateStageImportedFailsClosed(t *testing.T) {
	company := int64(1)
	decision := "match"
	in := CandidateStageInput{CandidateID: 1, TitleRevision: 1, TitleHash: "CI", CompanyID: &company, SourceID: "companySite", SourceSelected: true, ScopeAllowed: true, Roles: []CandidateStageRole{{ID: 1, PromptVersion: "CI", ModelKey: "CI", Decision: &decision}}, Snapshot: &CandidateStageSnapshot{ID: "CI"}, ExtractionID: "CI", LeafInputHash: "CI", Leaf: &CandidateStageLeaf{ID: "CI", InputHash: "CI", RoleIDs: []int64{1}}, Imported: true}
	if _, e := PlanCandidateStage(in); !errors.Is(e, ErrImportedCandidateResolution) {
		t.Fatal("imported candidate was guessed", e)
	}
}

func TestImportedCandidateNeverPlansEarlierStages(t *testing.T) {
	company := int64(1)
	match := "match"
	base := CandidateStageInput{CandidateID: 1, TitleRevision: 1, TitleHash: "CI", CompanyID: &company, SourceID: "companySite", SourceSelected: true, ScopeAllowed: true, Imported: true}
	for _, roles := range [][]CandidateStageRole{
		{{ID: 1, PromptVersion: "CI", ModelKey: "CI"}},
		{{ID: 1, PromptVersion: "CI", ModelKey: "CI", Decision: &match}},
	} {
		in := base
		in.Roles = roles
		plan, err := PlanCandidateStage(in)
		if plan != nil || !errors.Is(err, ErrImportedCandidateResolution) {
			t.Fatalf("imported candidate planned another stage: plan=%+v err=%v", plan, err)
		}
	}
}

func TestCandidateStageUncertainRefreshDoesNotProceed(t *testing.T) {
	company := int64(1)
	decision := "match"
	in := CandidateStageInput{CandidateID: 1, TitleRevision: 1, TitleHash: "CI", CompanyID: &company, SourceID: "companySite", SourceSelected: true, ScopeAllowed: true, Roles: []CandidateStageRole{{ID: 1, PromptVersion: "CI", ModelKey: "CI", Decision: &decision}}, Snapshot: &CandidateStageSnapshot{ID: "CI", FetchedAt: "2026-01-01"}, TitleUpdatedAt: "unparseable", ExtractionConfigHash: "CI", ExtractionPromptVersion: "CI"}
	if _, e := PlanCandidateStage(in); !errors.Is(e, ErrJobInput) {
		t.Fatal("unknown title freshness allowed downstream stage", e)
	}
}
