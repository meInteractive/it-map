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

func TestMariaDBContinuousWorkerHistoricalAIDisposable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		orphan bool
	}{{"orphan", true}, {"old_generation", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s, provider, worker := continuousWorkerFixture(t, false)
			runUntil := func(target workerloop.ProfileName) {
				ctx, cancel := context.WithCancel(s.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
						if name == target {
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
					t.Fatal("worker timed out")
				}
			}
			runUntil(workerloop.Launch)
			if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending'") == 0 {
				t.Fatal("synthetic classification fixture missing")
			}
			runID := "CI_missing_run"
			if !tc.orphan {
				old := "11111111-2222-4333-8444-555555555555"
				if old == s.repo.Generation {
					old = "11111111-2222-4333-8444-555555555556"
				}
				runID = "CI_historical_ai_run"
				s.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES(?,?,'automatic',1,'interrupted')", runID, old)
			}
			s.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_historical_ai','CI_other_job',?,'classify_title',1,'interrupted',0.25,NULL)", runID)
			runUntil(workerloop.Pipeline)
			deferred := s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='historical_ai_unknown' AND attempts=0")
			failed := s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='failed'")
			if provider.AICalls() != 0 || deferred == 0 || failed != 0 {
				t.Fatalf("unknown AI history: calls=%d deferred=%d failed=%d", provider.AICalls(), deferred, failed)
			}
			if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI_historical_ai' AND reserved_usd=0.25 AND cost_usd IS NULL") != 1 {
				t.Fatal("unknown reserve changed")
			}
			s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE reason_code='historical_ai_unknown'")
			runUntil(workerloop.Pipeline)
			repeated := s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='historical_ai_unknown' AND attempts=0")
			if provider.AICalls() != 0 || repeated != deferred {
				t.Fatalf("repeat AI guard: calls=%d deferred=%d expected=%d", provider.AICalls(), repeated, deferred)
			}
			// Подтверждение стоимости и сроки изменяются только в disposable CI.
			s.exec("UPDATE scheduler_attempts SET cost_usd=0.1 WHERE id='CI_historical_ai'")
			s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE reason_code='historical_ai_unknown'")
			runUntil(workerloop.Pipeline)
			if provider.AICalls() == 0 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 {
				t.Fatal("confirmed history did not allow AI to resume")
			}
		})
	}
}
