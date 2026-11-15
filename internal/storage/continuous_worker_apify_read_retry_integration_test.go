//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/integrations/apify"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func TestMariaDBContinuousWorkerApifyReadRetryDisposable(t *testing.T) {
	for _, stage := range []string{"account", "reconcile", "dataset", "retry_after", "status_408", "status_425"} {
		t.Run(stage, func(t *testing.T) {
			s, p, w := continuousWorkerFixture(t, false)
			transient := &apify.Error{Code: "apify_network", Status: 503}
			if stage == "account" {
				p.AccountError = transient
			}
			if stage == "reconcile" || stage == "retry_after" || stage == "status_408" || stage == "status_425" {
				p.Async = true
			}
			if stage == "dataset" {
				p.DatasetError = transient
			}
			runProfile := func(target workerloop.ProfileName) {
				ctx, cancel := context.WithCancel(s.ctx)
				done := make(chan error, 1)
				go func() {
					done <- (workerengine.Engine{Worker: w, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
						if name == target {
							cancel()
						}
					}}).Run(ctx)
				}()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("worker stopped", err)
					}
				case <-time.After(12 * time.Second):
					cancel()
					<-done
					t.Fatal("profile timed out")
				}
			}
			runProfile(workerloop.Launch)
			if stage == "retry_after" {
				transient = &apify.Error{Code: "apify_busy", Status: 503, RetryAfter: 3 * time.Minute}
			}
			if stage == "status_408" || stage == "status_425" {
				transient = &apify.Error{Code: "apify_data", Status: 503, RetryableRead: true, RetryAfter: 3 * time.Minute}
			}
			if stage == "reconcile" || stage == "retry_after" || stage == "status_408" || stage == "status_425" {
				if p.Starts() != 1 {
					t.Fatal("known Actor missing")
				}
				p.RunError = transient
				s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
				runProfile(workerloop.Reconcile)
			}
			expectedStarts := int64(1)
			if stage == "account" {
				expectedStarts = 0
			}
			if p.Starts() != expectedStarts || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code='deferred' AND attempts=0") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='failed'") != 0 {
				t.Fatal("temporary GET did not defer safely", p.Starts())
			}
			if (stage == "retry_after" || stage == "status_408" || stage == "status_425") && s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND run_after > CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)+150000") != 1 {
				t.Fatal("provider retry interval was ignored")
			}
			p.AccountError = nil
			p.RunError = nil
			p.DatasetError = nil
			s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
			ctx, cancel := context.WithCancel(s.ctx)
			done := make(chan error, 1)
			go func() { done <- (workerengine.Engine{Worker: w}).Run(ctx) }()
			timer := time.NewTimer(15 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			complete := false
		wait:
			for {
				select {
				case <-ticker.C:
					if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") == 1 {
						complete = true
						break wait
					}
				case <-timer.C:
					break wait
				}
			}
			timer.Stop()
			ticker.Stop()
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) || !complete || p.Starts() != 1 {
				t.Fatal("worker did not resume with one POST", err, p.Starts())
			}
		})
	}
}
