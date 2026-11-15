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

// Поворот поколения после принятого POST не даёт старому worker записать ответ.
func TestMariaDBContinuousWorkerGenerationPaidDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	provider.AfterStart = func(ctx context.Context) {
		once.Do(func() { close(reached) })
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	done := make(chan error, 1)
	outcome := make(chan workerloop.ProfileOutcome, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, result workerloop.ProfileOutcome) {
			if name == workerloop.Launch {
				select {
				case outcome <- result:
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
		t.Fatal("synthetic POST not reached")
	}
	defer s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", s.repo.Generation)
	s.exec("UPDATE app_runtime_generation SET generation='22222222-2222-4333-8444-555555555555' WHERE id=1")
	close(release)
	select {
	case got := <-outcome:
		if got != workerloop.ProfileFailed {
			t.Fatal("stale worker did not report failure", got)
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("stale worker did not finish")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("stale worker did not stop", err)
	}
	if provider.Starts() != 1 || provider.AICalls() != 0 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 {
		t.Fatal("stale worker recorded result or lost uncertain reserve")
	}
	if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 0 {
		t.Fatal("stale worker imported vacancy")
	}
	// Возврат поколения и ускорение истечения lease — только тестовый сценарий.
	s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", s.repo.Generation)
	s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
	ctx, cancel = context.WithCancel(s.ctx)
	done = make(chan error, 1)
	restarted := make(chan struct{}, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(name workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if name == workerloop.Launch {
				select {
				case restarted <- struct{}{}:
				default:
				}
			}
		}}).Run(ctx)
	}()
	select {
	case <-restarted:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("restart timed out")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("restarted worker did not stop", err)
	}
	if provider.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("restart repeated POST or lost reserve")
	}
}

// Worker завершается при новой generation даже с пустой очередью.
func TestMariaDBContinuousWorkerGenerationExitDisposable(t *testing.T) {
	s, provider, worker := continuousWorkerFixture(t, false)
	next := "22222222-2222-4333-8444-555555555555"
	if next == s.repo.Generation {
		next = "33333333-3333-4333-8444-555555555555"
	}
	defer s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", s.repo.Generation)
	s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", next)
	ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
	defer cancel()
	var failed int
	err := (workerengine.Engine{Worker: worker, OnResult: func(_ workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
		if outcome == workerloop.ProfileFailed {
			failed++
		}
	}}).Run(ctx)
	if !errors.Is(err, workerengine.ErrGenerationChanged) || failed != 1 {
		t.Fatal("stale worker did not exit", err, failed)
	}
	if provider.Starts() != 0 || provider.AICalls() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("stale worker contacted paid provider")
	}
}
