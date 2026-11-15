package parser

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadBackoffRetainsProviderMinimumAndBounds(t *testing.T) {
	for _, tc := range []struct {
		count      int
		hint, want time.Duration
	}{
		{0, time.Minute, time.Minute}, {1, time.Minute, 2 * time.Minute},
		{3, 3 * time.Minute, 8 * time.Minute}, {10, time.Minute, 32 * time.Minute},
		{2, 4 * time.Hour, 4 * time.Hour}, {-1, 0, time.Minute},
	} {
		if got := readBackoff(tc.count, tc.hint); got != tc.want {
			t.Fatalf("count=%d hint=%v got=%v want=%v", tc.count, tc.hint, got, tc.want)
		}
	}
}

func TestRecentReadFailuresRequiresOwnedAttempt(t *testing.T) {
	if _, err := recentReadFailures(context.Background(), nil, "CI", apifyReadRetryReason); !errors.Is(err, ErrJobInput) {
		t.Fatal("unowned retry history accepted", err)
	}
}
