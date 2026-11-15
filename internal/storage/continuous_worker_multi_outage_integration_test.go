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
		done := make(chan error, 1)
		go func() { done <- (workerengine.Engine{Worker: w}).Run(ctx) }()
		timer := time.NewTimer(20 * time.Second)
		tick := time.NewTicker(100 * time.Millisecond)
		reached := false
	wait:
		for {
			select {
			case <-tick.C:
				if ready() {
					reached = true
					break wait
				}
			case err := <-done:
				cancel()
				timer.Stop()
				tick.Stop()
				t.Fatal("worker stopped before milestone", err)
			case <-timer.C:
				break wait
			}
		}
		timer.Stop()
		tick.Stop()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || !reached {
			t.Fatal("worker milestone failed", err)
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
	if p.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE status='failed'") != 0 {
		t.Fatal("recovery did not finish with one Actor")
	}
}
