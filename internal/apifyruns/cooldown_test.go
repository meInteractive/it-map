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
		{"expired", now - 60000, 0}, {"almost expired", now - 59999, time.Millisecond},
		{"twenty seconds remaining", now - 40000, 20 * time.Second}, {"new result", now, time.Minute},
		{"future clock", now + 1, time.Minute}, {"extreme future", 1 << 62, time.Minute},
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
