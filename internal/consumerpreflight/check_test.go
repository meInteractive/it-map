package consumerpreflight

import (
	"context"
	"errors"
	"testing"
)

func TestCheckCandidateRejectsUnavailableInputs(t *testing.T) {
	if err := CheckCandidate(nil, nil, ""); !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
	if err := CheckCandidate(context.Background(), nil, "11111111-2222-4333-8444-555555555555"); !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
}
