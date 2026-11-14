//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/workerconfig"
	"itmap/internal/workerengine"
)

func TestMariaDBContinuousWorkerDrainDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	accepted := make(chan struct{})
	release := make(chan struct{})
	provider.AfterStart = func(ctx context.Context) {
		close(accepted)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	cfg := workerconfig.Defaults()
	cfg.ShutdownGrace = 5 * time.Second
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (workerengine.Engine{Worker: worker, Config: &cfg}).Run(ctx) }()
	select {
	case <-accepted:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("synthetic POST not reached")
	}
	cancel()
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("draining worker did not stop")
	}
	if provider.Starts() != 1 || provider.AICalls() != 0 {
		t.Fatal("worker started additional work after stop")
	}
	if s.count("SELECT COUNT(*) FROM apify_runs WHERE state='succeeded' AND run_id='MockRun0' AND charged_usd IS NOT NULL") != 1 {
		t.Fatal("active actor result was not saved within grace")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts a JOIN scheduler_jobs j ON j.id=a.job_id WHERE j.type <> 'scan_source'") != 0 {
		t.Fatal("worker claimed a new pipeline job during drain")
	}
}
