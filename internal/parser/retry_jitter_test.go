package parser

import (
	"fmt"
	"testing"
	"time"
)

func TestDeferredRetrySpreadKeepsProviderMinimumAndBounds(t *testing.T) {
	base, limit := time.Minute, 24*time.Hour
	distinct := map[time.Duration]bool{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("CIjob%d", i)
		got := spreadDeferredRetry(id, "hh-public", base, limit)
		if got < base || got > base+base/4 || got != spreadDeferredRetry(id, "hh-public", base, limit) {
			t.Fatal("invalid retry bound or instability", got)
		}
		distinct[got] = true
	}
	if len(distinct) < 2 {
		t.Fatal("independent jobs were not spread")
	}
	if got := spreadDeferredRetry("CI", "apify", limit, limit); got != limit {
		t.Fatal("limit changed", got)
	}
	if got := spreadDeferredRetry("CI", "apify", 3*time.Minute, 3*time.Minute+time.Second); got < 3*time.Minute || got > 3*time.Minute+time.Second {
		t.Fatal("remaining limit exceeded", got)
	}
}
