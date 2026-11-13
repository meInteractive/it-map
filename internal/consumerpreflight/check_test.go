package consumerpreflight

import (
	"context"
	"database/sql"
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

func TestPreflightRejectsMalformedGeneration(t *testing.T) {
	// Пустой DB не используется: ошибка должна обнаружиться до обращения к нему.
	db := new(sql.DB)
	for _, check := range []func(context.Context, *sql.DB, string) error{CheckBaseline, CheckCandidate, CheckVersionedCandidate} {
		for _, generation := range []string{"", "not-a-generation", "11111111-2222-4333-8444-55555555555F"} {
			if err := check(context.Background(), db, generation); !errors.Is(err, ErrInput) {
				t.Fatalf("invalid generation accepted: %v", err)
			}
		}
	}
}
