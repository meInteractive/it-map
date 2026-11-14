//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/parser"
	"itmap/internal/parser/stagingmock"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func TestMariaDBContinuousWorkerDisabledDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	if enabled, err := f.repo.SchedulerEnabledSnapshot(f.ctx); err != nil || !enabled {
		t.Fatal("enabled snapshot missing", err)
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false}' WHERE id=1`)
	if enabled, err := f.repo.SchedulerEnabledSnapshot(f.ctx); err != nil || enabled {
		t.Fatal("disabled snapshot incorrect", err)
	}
	provider := &stagingmock.Providers{}
	worker, err := stagingmock.Worker(f.repo, "CI_SECRET", provider)
	if err != nil {
		t.Fatal("synthetic worker unavailable", err)
	}
	beforeRuns := f.count("SELECT COUNT(*) FROM scheduler_runs")
	beforeJobs := f.count("SELECT COUNT(*) FROM scheduler_jobs")
	beforeLedger := f.count("SELECT COUNT(*) FROM apify_runs")
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		done := make(chan error, 1)
		outcome := make(chan workerloop.ProfileOutcome, 1)
		go func() {
			done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, result workerloop.ProfileOutcome) {
				if name == workerloop.Launch {
					outcome <- result
					cancel()
				}
			}}).Run(ctx)
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("worker did not stop", err)
			}
		case <-time.After(10 * time.Second):
			cancel()
			<-done
			t.Fatal("disabled worker timed out")
		}
		cancel()
		if result := <-outcome; result != workerloop.ProfileFailed {
			t.Fatal("disabled scheduler reported ready", result)
		}
	}
	if f.count("SELECT COUNT(*) FROM scheduler_runs") != beforeRuns || f.count("SELECT COUNT(*) FROM scheduler_jobs") != beforeJobs || f.count("SELECT COUNT(*) FROM apify_runs") != beforeLedger || provider.Starts() != 0 || provider.AICalls() != 0 {
		t.Fatal("disabled polling produced work or writes")
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":"invalid"}' WHERE id=1`)
	if _, err := f.repo.SchedulerEnabledSnapshot(f.ctx); !errors.Is(err, parser.ErrRunStorage) {
		t.Fatal("invalid settings accepted", err)
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	if enabled, err := f.repo.SchedulerEnabledSnapshot(f.ctx); err != nil || !enabled {
		t.Fatal("re-enabled scheduler not observed", err)
	}
	// Отключение между снимком и Begin не выдаёт право выполнения.
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false}' WHERE id=1`)
	start, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI snapshot race"})
	if err != nil || start.Acquired || start.Run.Status != "paused" {
		t.Fatal("stale snapshot authorized work", err)
	}
	stale := f.repo
	stale.Generation = "11111111-2222-4333-8444-555555555555"
	if stale.Generation == f.repo.Generation {
		stale.Generation = "11111111-2222-4333-8444-555555555556"
	}
	if _, err := stale.SchedulerEnabledSnapshot(f.ctx); !errors.Is(err, parser.ErrRunGeneration) {
		t.Fatal("stale generation accepted", err)
	}
}
