package parser

import (
	"errors"
	"testing"
)

func TestImportStageCurrentJob(t *testing.T) {
	company, candidate, source := int64(12), int64(34), "hh"
	match := "match"
	input := CandidateStageInput{CandidateID: candidate, CompanyID: &company, SourceID: source, SourceSelected: true, ScopeAllowed: true, TitleRevision: 1, TitleHash: "hash", Roles: []CandidateStageRole{{ID: 1, PromptVersion: "p", ModelKey: "m", Decision: &match}}, Snapshot: &CandidateStageSnapshot{ID: "snapshot"}, ExtractionID: "extraction", LeafInputHash: "leaf-hash", Leaf: &CandidateStageLeaf{ID: "roles", InputHash: "leaf-hash", RoleIDs: []int64{2}}}
	plan, err := PlanCandidateStage(input)
	if err != nil || plan == nil {
		t.Fatal(err)
	}
	job := Job{Type: "import_vacancy", CompanyID: &company, CandidateID: &candidate, SourceID: &source, DedupeKey: stageHash(plan.Key)}
	if err = validateImportStageJob(job, input, false); err != nil {
		t.Fatal(err)
	}
	stale := job
	stale.DedupeKey = stageHash("old")
	if !errors.Is(validateImportStageJob(stale, input, false), ErrCandidateChanged) {
		t.Fatal("accepted stale dedupe")
	}
	missing := input
	missing.Leaf = &CandidateStageLeaf{ID: "roles", InputHash: "leaf-hash"}
	if !errors.Is(validateImportStageJob(job, missing, false), ErrCandidateChanged) {
		t.Fatal("accepted empty roles")
	}
	imported := input
	imported.Imported = true
	if !errors.Is(validateImportStageJob(job, imported, false), ErrCandidateChanged) {
		t.Fatal("accepted imported candidate without receipt")
	}
	if err = validateImportStageJob(job, imported, true); err != nil {
		t.Fatal(err)
	}
	other := int64(35)
	wrong := job
	wrong.CandidateID = &other
	if !errors.Is(validateImportStageJob(wrong, input, false), ErrCandidateChanged) {
		t.Fatal("accepted different candidate")
	}
}
