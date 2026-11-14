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

func TestMariaDBContinuousWorkerCurrentUnknownDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	s.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('CI_current_unknown',?,'CI_other_job','free','StagingMock','CIhash','CIstart','CIend',0.25,'uncertain',1,1)`, s.repo.Generation)
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	finished := make(chan struct{}, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if name == workerloop.Launch {
				select {
				case finished <- struct{}{}:
				default:
				}
			}
		}}).Run(ctx)
	}()
	select {
	case <-finished:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("worker did not finish launch")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not stop", err)
	}
	if provider.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("worker started a new paid operation")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code='current_paid_unknown' AND run_after>0 AND attempts=0") != 1 {
		t.Fatal("worker did not safely defer blocked job")
	}
}
