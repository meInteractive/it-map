//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/workerconfig"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func TestMariaDBContinuousWorkerDrainPaidDisposable(t *testing.T) {
	t.Run("before_reserve", func(t *testing.T) {
		s, provider, worker := continuousWorkerFixture(t, false)
		reached, release := make(chan struct{}), make(chan struct{})
		provider.BeforeAccount = func(ctx context.Context) {
			close(reached)
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
		case <-reached:
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("account request not reached")
		}
		cancel()
		close(release)
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("worker did not stop", err)
			}
		case <-time.After(12 * time.Second):
			t.Fatal("worker did not stop after account request")
		}
		if provider.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
			t.Fatal("worker started or reserved a new actor after stop")
		}
	})
	t.Run("timeout_restart", func(t *testing.T) {
		s, provider, worker := continuousWorkerFixture(t, false)
		reached := make(chan struct{})
		var once sync.Once
		provider.AfterStart = func(ctx context.Context) { once.Do(func() { close(reached) }); <-ctx.Done() }
		cfg := workerconfig.Defaults()
		cfg.ShutdownGrace = time.Second
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- (workerengine.Engine{Worker: worker, Config: &cfg}).Run(ctx) }()
		select {
		case <-reached:
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("synthetic POST not reached")
		}
		cancel()
		select {
		case err := <-done:
			t.Fatal("grace was not given to accepted POST", err)
		case <-time.After(100 * time.Millisecond):
		}
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("worker did not stop", err)
			}
		case <-time.After(12 * time.Second):
			t.Fatal("worker did not stop after grace")
		}
		if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 {
			t.Fatal("timed out POST did not retain unknown reserve")
		}
		s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
		s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
		s.exec("UPDATE scheduler_jobs SET status='pending',run_after=0 WHERE type='scan_source' AND status='failed'")
		s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
		if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status IN ('pending','running')") != 1 {
			t.Fatal("synthetic retry fixture missing")
		}
		ctx, cancel = context.WithCancel(s.ctx)
		done = make(chan error, 1)
		launched := make(chan struct{}, 1)
		go func() {
			done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				if name == workerloop.Launch {
					select {
					case launched <- struct{}{}:
					default:
					}
				}
			}}).Run(ctx)
		}()
		select {
		case <-launched:
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
			t.Fatal("restart repeated POST or lost reserve")
		}
	})
}
