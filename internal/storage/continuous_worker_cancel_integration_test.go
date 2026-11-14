//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

// Отмена во время принятого POST не даёт повторить платную операцию после рестарта.
func TestMariaDBContinuousWorkerCancelPaidDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	accepted := make(chan struct{})
	var once sync.Once
	provider.AfterStart = func(ctx context.Context) { once.Do(func() { close(accepted) }); <-ctx.Done() }
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	go func() { done <- (workerengine.Engine{Worker: worker}).Run(ctx) }()
	select {
	case <-accepted:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("synthetic POST was not reached")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled worker did not stop", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("worker did not finish cancellation cleanup")
	}
	if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 {
		t.Fatal("cancelled POST did not preserve unknown paid outcome")
	}
	// Изменение сроков применяется только в одноразовой базе CI.
	s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET status='pending',run_after=0 WHERE type='scan_source' AND status='failed'")
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status IN ('pending','running')") != 1 {
		t.Fatal("synthetic retry fixture missing")
	}
	ctx, cancel = context.WithCancel(s.ctx)
	done = make(chan error, 1)
	launch := make(chan struct{}, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if name == workerloop.Launch {
				select {
				case launch <- struct{}{}:
				default:
				}
			}
		}}).Run(ctx)
	}()
	select {
	case <-launch:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("restart recovery timed out")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("restarted worker did not stop", err)
	}
	if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("restart repeated POST or lost its reserve")
	}
}
