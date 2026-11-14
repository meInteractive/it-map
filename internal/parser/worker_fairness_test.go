package parser

import (
	"math"
	"testing"
	"time"
)

func TestWorkerPipelineDoesNotStarveOldEligibleJobs(t *testing.T) {
	now := int64(100 * time.Hour / time.Millisecond)
	old := Job{ID: "old", Priority: 0, CreatedAt: now - int64(time.Hour/time.Millisecond), RunAfter: now - int64(31*time.Minute/time.Millisecond)}
	fresh := Job{ID: "fresh", Priority: 100, CreatedAt: now, RunAfter: now - int64(time.Minute/time.Millisecond)}
	if claimBefore(old, false, fresh, false, now) {
		t.Fatal("fixture should prefer fresh priority in legacy policy")
	}
	if !claimBeforeForWorker(old, false, fresh, false, now, true) || claimBeforeForWorker(fresh, false, old, false, now, true) {
		t.Fatal("old work was starved")
	}
	if claimBeforeForWorker(old, false, fresh, false, now, false) {
		t.Fatal("legacy ordering changed")
	}
	older := old
	older.ID = "older"
	older.RunAfter--
	older.Priority = -100
	if !claimBeforeForWorker(older, false, old, false, now, true) {
		t.Fatal("oldest due work was not selected")
	}
	old.RunAfter = now - int64(29*time.Minute/time.Millisecond)
	if claimBeforeForWorker(old, false, fresh, false, now, true) {
		t.Fatal("fresh priorities not preserved")
	}
	if claimBeforeForWorker(old, false, fresh, false, math.MinInt64, true) != claimBefore(old, false, fresh, false, math.MinInt64) {
		t.Fatal("overflow changed selection")
	}
}
