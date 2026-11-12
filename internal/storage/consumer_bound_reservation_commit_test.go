//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/parser"
)

// Повторяет реальный COMMIT и искусственно теряет его подтверждение. Только CI.
func testBoundReservationLostCommit(t *testing.T, f *schedulerFixture, failInterrupt bool) {
	t.Helper()
	billingReset(f)
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='go_consumer_control'") != 0 {
		t.Fatal("control table already exists")
	}
	f.exec(`CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_control"); err != nil {
			t.Error("control cleanup failed")
		}
	})
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO go_consumer_control(id,generation,revision,mode,owner_digest,lease_until) VALUES(1,?,1,'worker',?,?)", f.repo.Generation, digest[:], schedulerClock(t, f.db)+60000)
	prefix := "UPDATE scheduler_attempts SET reserved_usd=reserved_usd+"
	if failInterrupt {
		prefix = "UPDATE scheduler_attempts SET finished_at=?"
	}
	repo, fired := billingCommitRepository(t, f.repo.Generation, prefix)
	req := consumercontrol.Request{Generation: f.repo.Generation, Revision: 1, Origin: consumercontrol.WorkerRun, Token: token}
	run, bound, err := repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI_bound_lost_commit", Diagnostic: true, Duration: time.Second}, req)
	if err != nil || bound == nil || !run.Acquired {
		t.Fatal("bound run", err)
	}
	jobID := "CI_bound_lost_commit_job"
	schedulerSeedJob(f, jobID, nil)
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 10 * time.Second})
	if err != nil || lease == nil {
		t.Fatal("bound claim", err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil || attempt == nil {
		t.Fatal("bound attempt", err)
	}
	err = attempt.Reserve(f.ctx, syntheticPaid)
	if failInterrupt {
		if err != nil || fired.Load() {
			t.Fatal("reservation failed before interruption", err)
		}
	} else if !errors.Is(err, parser.ErrRunStorage) || !fired.Load() {
		t.Fatal("lost reservation commit was not reproduced", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL AND status='running'", jobID) != 1 {
		t.Fatal("committed reservation lost")
	}
	if err = attempt.Reserve(f.ctx, syntheticPaid); !errors.Is(err, parser.ErrMoneyUncertain) {
		t.Fatal("unknown commit was replayed", err)
	}
	err = attempt.Interrupt(f.ctx)
	if failInterrupt {
		if !errors.Is(err, parser.ErrRunStorage) || !fired.Load() {
			t.Fatal("lost interruption commit was not reproduced", err)
		}
		if err = attempt.Interrupt(f.ctx); !errors.Is(err, parser.ErrJobLeaseLost) {
			t.Fatal("committed interruption was replayed", err)
		}
	} else if err != nil {
		t.Fatal("interrupt", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL AND status='interrupted'", jobID) != 1 {
		t.Fatal("interruption lost reserve")
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='failed'", jobID) != 1 {
		t.Fatal("job not fenced")
	}
}
