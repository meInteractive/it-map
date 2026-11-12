//go:build mariadb && schedulerwrites

package storage

import (
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
	run, err := f.repo.BeginGuarded(f.ctx, options, req)
	if err != nil || !run.Acquired || run.Reused {
		t.Fatal("guarded run not acquired", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", run.Run.ID)
	replay, err := f.repo.BeginGuarded(f.ctx, options, req)
	if err != nil || !replay.Reused || replay.Acquired {
		t.Fatal("replay acquired run", err)
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
