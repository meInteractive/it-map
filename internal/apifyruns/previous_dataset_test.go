package apifyruns

import (
	"context"
	"errors"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

func TestPreviousDatasetRejectsChangedIdentityBeforeStorage(t *testing.T) {
	previous := apifysettings.OperationVersion{Generation: "11111111-1111-4111-8111-111111111111", CompanyID: 1, SourceURL: "https://hh.ru/employer/456", SourceRevision: 1, SettingsRevision: 1, Profile: "free"}
	for _, change := range []func(*apifysettings.OperationVersion){
		func(v *apifysettings.OperationVersion) { v.Generation = "22222222-2222-4222-8222-222222222222" },
		func(v *apifysettings.OperationVersion) { v.CompanyID++ },
		func(v *apifysettings.OperationVersion) { v.SourceURL = "https://hh.ru/employer/457" },
		func(v *apifysettings.OperationVersion) { v.Profile = "starter" },
		func(v *apifysettings.OperationVersion) { v.SourceRevision = 0 },
		func(v *apifysettings.OperationVersion) { v.SettingsRevision = 0 },
		func(v *apifysettings.OperationVersion) { v.CandidateID = 1 },
	} {
		current := previous
		change(&current)
		_, err := (Service{}).ReadPreviousDataset(context.Background(), previous, current, "CI", apify.HHOptions{}, 0, "CIRun", "CIData")
		if !errors.Is(err, apifysettings.ErrChanged) {
			t.Fatalf("changed identity admitted: %v", err)
		}
	}
	if _, err := (Service{}).ReadPreviousDataset(context.Background(), previous, previous, "CI", apify.HHOptions{Detail: true}, 0, "CIRun", "CIData"); !errors.Is(err, apifysettings.ErrChanged) {
		t.Fatalf("detail operation admitted: %v", err)
	}
}

func TestDatasetInvalidOffsetPrecedesOperationValidation(t *testing.T) {
	_, err := (Service{}).ReadDataset(context.Background(), apifysettings.OperationVersion{}, "", apify.HHOptions{}, -1)
	if !errors.Is(err, ErrDatasetChanged) {
		t.Fatalf("invalid offset contract changed: %v", err)
	}
}
