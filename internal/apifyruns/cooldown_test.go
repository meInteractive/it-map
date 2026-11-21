package apifyruns

import (
	"errors"
	"testing"
	"time"
)

func TestCooldownRetryDelay(t *testing.T) {
	now := int64(2000000)
	for _, tc := range []struct {
		name string
		last int64
		want time.Duration
	}{
		{"expired", now - 300000, 0}, {"almost expired", now - 299999, time.Millisecond},
		{"one minute remaining", now - 240000, time.Minute}, {"new result", now, 5 * time.Minute},
		{"future clock", now + 1, 5 * time.Minute}, {"extreme future", 1 << 62, 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryAfterCooldown(tc.last, now); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	err := &CooldownError{RetryAfter: time.Second}
	if !errors.Is(err, ErrCooldown) {
		t.Fatal("sentinel lost")
	}
}
