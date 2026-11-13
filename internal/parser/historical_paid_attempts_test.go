package parser

import (
	"context"
	"errors"
	"testing"
)

func TestHistoricalPaidAttemptsRejectsMissingInputs(t *testing.T) {
	if _, err := (RunRepository{}).InspectHistoricalPaidAttempts(nil); !errors.Is(err, ErrRunInput) {
		t.Fatal("missing context accepted", err)
	}
	if _, err := (RunRepository{Generation: "11111111-2222-4333-8444-555555555555"}).InspectHistoricalPaidAttempts(context.Background()); !errors.Is(err, ErrRunInput) {
		t.Fatal("missing DB accepted", err)
	}
}
