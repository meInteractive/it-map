//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/integrations/apify"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

// A single synthetic job crosses several outages and still reaches import with
// one Actor. Deadlines are advanced only in the disposable CI database.
func TestMariaDBContinuousWorkerMultiOutageDisposable(t *testing.T) {
	s, p, w := continuousWorkerFixture(t, false)
	transient := &apify.Error{Code: "apify_network", Status: 503}
	p.AccountError = transient
	runUntil := func(ready func() bool) {
		t.Helper()
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		done := make(chan error, 1)
		reached := make(chan struct{}, 1)
		go func() {
			done <- (workerengine.Engine{Worker: w, OnResult: func(_ workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				// Check after the whole cycle has finished, then cancel before
				// the loop can claim another job. Polling DB from another
				// goroutine could cancel an unrelated in-flight stage.
				if ready() {
					select {
					case reached <- struct{}{}:
					default:
					}
					cancel()
				}
			}}).Run(ctx)
		}()
		select {
		case err := <-done:
			select {
			case <-reached:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("worker milestone failed", err)
				}
			default:
				t.Fatal("worker stopped before milestone", err)
			}
		case <-time.After(20 * time.Second):
			cancel()
			<-done
			t.Fatal("worker milestone timed out")
		}
	}
	pendingScan := func() bool {
		return s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code IN ('deferred','apify_read_retry') AND attempts=0") == 1
	}
	dueScan := func() { s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'") }
	runUntil(pendingScan)
	if p.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("account outage started Actor")
	}
	p.AccountError = nil
	p.Async = true
	p.RunError = transient
	dueScan()
	runUntil(func() bool {
		return p.Starts() == 1 && s.count("SELECT COUNT(*) FROM apify_runs WHERE state='running' AND run_id='MockRun0'") == 1
	})
	dueScan()
	runUntil(func() bool { return p.Polls() > 0 && pendingScan() })
	if p.Starts() != 1 {
		t.Fatal("reconcile outage repeated Actor")
	}
	p.RunError = nil
	p.DatasetError = transient
	dueScan()
	runUntil(func() bool {
		return s.count("SELECT COUNT(*) FROM apify_runs WHERE state='succeeded' AND charged_usd IS NOT NULL") == 1 && pendingScan()
	})
	if p.Starts() != 1 {
		t.Fatal("dataset outage repeated Actor")
	}
	p.DatasetError = nil
	p.PublicError = hhpublic.ErrTemporary
	dueScan()
	runUntil(func() bool {
		return s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='fetch_full_text' AND status='pending' AND reason_code IN ('deferred','hh_public_retry') AND attempts=0") == 1
	})
	if p.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("public outage triggered paid fallback")
	}
	p.PublicError = nil
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='fetch_full_text' AND status='pending'")
	runUntil(func() bool {
		return s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") == 1
	})
	starts := p.Starts()
	runs := s.count("SELECT COUNT(*) FROM apify_runs")
	failed := s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE status='failed'")
	if starts != 1 || runs != 1 || failed != 0 {
		t.Fatalf("recovery: starts=%d runs=%d failed jobs=%d", starts, runs, failed)
	}
}
