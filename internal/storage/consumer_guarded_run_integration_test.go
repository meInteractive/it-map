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
	claim.Lease = 10 * time.Second
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
	if err := bound.ReleaseUnstarted(f.ctx, nil); !errors.Is(err, parser.ErrJobInput) {
		t.Fatal("missing lease accepted", err)
	}
	f.exec("UPDATE scheduler_jobs SET locked_until=? WHERE id=?", run.Run.LeaseUntil+1, jobID)
	if err := bound.ReleaseUnstarted(f.ctx, lease); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("job outliving run was released", err)
	}
	f.exec("UPDATE scheduler_jobs SET locked_until=? WHERE id=?", lease.Job().LockedUntil, jobID)
	f.exec("UPDATE scheduler_runs SET lease_until=? WHERE id=?", run.Run.LeaseUntil+300000, run.Run.ID)
	if err := bound.ReleaseUnstarted(f.ctx, lease); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("run outliving consumer was released", err)
	}
	f.exec("UPDATE scheduler_runs SET lease_until=? WHERE id=?", run.Run.LeaseUntil, run.Run.ID)
	f.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,result_json) VALUES('CI_bound_reserved',?,?,'diagnostic',?,'running',0.26,'{"synthetic":"retained"}')`, jobID, run.Run.ID, run.Run.StartedAt)
	if err := bound.ReleaseUnstarted(f.ctx, lease); !errors.Is(err, parser.ErrBoundJobUnavailable) {
		t.Fatal("release accepted a run with an attempt", err)
	}
	f.assertAttempt("CI_bound_reserved", "running")
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_bound_claim' AND status='running'") != 1 {
		t.Fatal("failed release changed job")
	}
	f.exec("DELETE FROM scheduler_attempts WHERE id='CI_bound_reserved'")
	f.exec("UPDATE go_consumer_control SET mode='draining', revision=4 WHERE id=1")
	if err := bound.ReleaseUnstarted(f.ctx, lease); err != nil {
		t.Fatal("draining could not release unstarted job", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_bound_claim' AND status='pending' AND attempts=0 AND locked_by='' AND locked_until=0") != 1 {
		t.Fatal("unstarted job was not returned safely")
	}
	if err := bound.ReleaseUnstarted(f.ctx, lease); !errors.Is(err, parser.ErrJobLeaseLost) {
		t.Fatal("released lease accepted twice", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker', revision=3 WHERE id=1")
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
