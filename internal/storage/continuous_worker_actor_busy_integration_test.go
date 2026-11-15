//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func TestMariaDBContinuousWorkerActorBusyDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	s.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('CI_busy_actor',?,'CI_busy_other','free','StagingMock','CIhash','CIstart','CIend',0.25,'running',CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED),1)`, s.repo.Generation)
	runLaunch := func() {
		ctx, cancel := context.WithCancel(s.ctx)
		done := make(chan error, 1)
		reached := make(chan struct{}, 1)
		go func() {
			done <- (workerengine.Engine{Worker: worker, OnResult: func(p workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				if p == workerloop.Launch {
					select {
					case reached <- struct{}{}:
					default:
					}
				}
			}}).Run(ctx)
		}()
		select {
		case <-reached:
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("launch timed out")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop", err)
		}
	}
	runLaunch()
	if provider.Starts() != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code='deferred' AND attempts=0") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='failed'") != 0 {
		t.Fatal("busy Actor did not defer safely")
	}
	// Подтверждение и ускорение времени разрешены только в одноразовой CI БД.
	s.exec("UPDATE apify_runs SET state='succeeded',run_id='CIResolved',dataset_id='CIData',charged_usd=0.1,created_at=1,updated_at=1 WHERE id='CI_busy_actor'")
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source'")
	runLaunch()
	if provider.Starts() != 1 {
		t.Fatal("worker did not resume after busy Actor resolved")
	}
}
