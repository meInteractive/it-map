//go:build mariadb && schedulerwrites

package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/parser"
)

func testGuardedRun(t *testing.T, f *schedulerFixture, req consumercontrol.Request) {
	t.Helper()
	before := f.count("SELECT COUNT(*) FROM scheduler_runs")
	options := parser.RunOptions{RequestID: "CI_guarded_run", Diagnostic: true, Duration: time.Second}
	wrong := req
	wrong.Token, _ = consumercontrol.NewToken()
	if _, err := f.repo.BeginGuarded(f.ctx, options, wrong); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("wrong owner accepted", err)
	}
	tooLong := options
	tooLong.Duration = 2 * time.Minute
	if _, err := f.repo.BeginGuarded(f.ctx, tooLong, req); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("run outlived owner", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_runs") != before {
		t.Fatal("failed guard committed a run")
	}
	run, bound, err := f.repo.BeginBoundRun(f.ctx, options, req)
	if err != nil || !run.Acquired || run.Reused || bound == nil {
		t.Fatal("guarded run not acquired", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", run.Run.ID)
	replay, replayHandle, err := f.repo.BeginBoundRun(f.ctx, options, req)
	if err != nil || !replay.Reused || replay.Acquired || replayHandle != nil {
		t.Fatal("replay acquired run", err)
	}
	jobID := "CI_bound_claim"
	schedulerSeedJob(f, jobID, nil)
	defer f.exec("DELETE FROM scheduler_jobs WHERE id=?", jobID)
	claim := parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: time.Minute}
	if _, err := bound.ClaimJob(f.ctx, claim); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("claim exceeded run lease", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_bound_claim' AND status='pending' AND attempts=0") != 1 {
		t.Fatal("rejected claim did not roll back")
	}
	f.exec("UPDATE go_consumer_control SET mode='draining', revision=4 WHERE id=1")
	claim.Lease = time.Second
	if _, err := bound.ClaimJob(f.ctx, claim); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("draining accepted claim", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker', revision=3 WHERE id=1")
	lease, err := bound.ClaimJob(f.ctx, claim)
	if err != nil || lease == nil || lease.Job().ID != jobID {
		t.Fatal("bounded claim failed", err)
	}
	if _, err := lease.StartAttempt(f.ctx); !errors.Is(err, parser.ErrBoundJobUnavailable) {
		t.Fatal("bound job started attempt without consumer fence", err)
	}
	called := false
	if err := lease.WithinOwned(f.ctx, func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, parser.ErrBoundJobUnavailable) || called {
		t.Fatal("bound job executed unfenced transaction", err)
	}
	if err := lease.AssertActive(f.ctx); !errors.Is(err, parser.ErrBoundJobUnavailable) {
		t.Fatal("bound job allowed external work", err)
	}
	if err := lease.Finish(f.ctx, parser.FinishOptions{}); !errors.Is(err, parser.ErrBoundJobUnavailable) {
		t.Fatal("bound job finished without consumer fence", err)
	}
	if err := f.repo.Finish(f.ctx, run.Run.ID, "completed"); err != nil {
		t.Fatal("finish guarded run", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='draining', revision=4 WHERE id=1")
	draining := req
	draining.Revision = 4
	if _, err := f.repo.BeginGuarded(f.ctx, options, draining); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("draining accepted replay", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker', revision=3 WHERE id=1")
}
