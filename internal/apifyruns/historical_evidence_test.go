package apifyruns

import (
	"context"
	"errors"
	"testing"
)

func TestHistoricalEvidenceRejectsUnavailableInputs(t *testing.T) {
	if _, err := (Repository{}).InspectHistoricalEvidence(nil, ""); !errors.Is(err, ErrStorage) {
		t.Fatal("missing input accepted", err)
	}
	if _, err := (Repository{}).InspectHistoricalEvidence(context.Background(), "11111111-2222-4333-8444-555555555555"); !errors.Is(err, ErrStorage) {
		t.Fatal("missing DB accepted", err)
	}
	if _, err := InspectHistoricalEvidenceTx(context.Background(), nil, "11111111-2222-4333-8444-555555555555"); !errors.Is(err, ErrStorage) {
		t.Fatal("missing transaction accepted", err)
	}
}
