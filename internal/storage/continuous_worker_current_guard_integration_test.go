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
	for _, tc := range []struct{ name, state, run, dataset string }{
		{"uncertain_without_id", "uncertain", "", ""},
		{"old_running_known", "running", "CIRun", "CIData"},
		{"terminal_missing_cost", "succeeded", "CIRun", "CIData"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, provider, worker := continuousWorkerFixture(t, false)
			s.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('CI_current_unknown',?,'CI_other_job','free','StagingMock','CIhash','CIstart','CIend',0.25,?,?,?,1,1)`, s.repo.Generation, tc.state, tc.run, tc.dataset)
			runLaunch := func() {
				t.Helper()
				ctx, cancel := context.WithCancel(s.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
						if name == workerloop.Launch {
							cancel()
						}
					}}).Run(ctx)
				}()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("worker did not stop", err)
					}
				case <-time.After(12 * time.Second):
					cancel()
					<-done
					t.Fatal("worker did not finish launch")
				}
			}
			for i := 0; i < 2; i++ {
				runLaunch()
				if provider.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code='current_paid_unknown' AND run_after>0 AND attempts=0") != 1 {
					t.Fatal("unknown current operation did not block new paid launch")
				}
				s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
			}
			// Synthetic confirmation only; production requires verified provider evidence.
			s.exec("UPDATE apify_runs SET state='succeeded',run_id='CIRun',dataset_id='CIData',charged_usd=0.02 WHERE id='CI_current_unknown'")
			runLaunch()
			if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 2 {
				t.Fatal("confirmed cost did not permit one new launch")
			}
		})
	}
}
