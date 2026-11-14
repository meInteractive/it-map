//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/workerconfig"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func TestMariaDBContinuousWorkerConfigDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	cfg := workerconfig.Defaults()
	cfg.MaxJobs = 2
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	failed := make(chan struct{}, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, Config: &cfg, OnResult: func(_ workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
			if outcome == workerloop.ProfileFailed || outcome == workerloop.ProfileTimeout {
				select {
				case failed <- struct{}{}:
				default:
				}
			}
		}}).Run(ctx)
	}()
	completed := false
	deadline := time.NewTimer(15 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
wait:
	for {
		select {
		case <-ticker.C:
			if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") == 1 {
				completed = true
				break wait
			}
		case <-failed:
			break wait
		case <-deadline.C:
			break wait
		case <-s.ctx.Done():
			break wait
		}
	}
	ticker.Stop()
	deadline.Stop()
	if completed {
		select {
		case <-time.After(time.Second):
		case <-s.ctx.Done():
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not stop", err)
	}
	if !completed || provider.Starts() != 1 {
		t.Fatal("configured worker did not complete synthetic pipeline exactly once")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='completed'") < 4 {
		t.Fatal("configured batch size did not split pipeline across cycles")
	}
}
