//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
)

// Только одноразовая CI MariaDB, проверенная openSchedulerFixture; это не миграция.
func testConsumerGuard(t *testing.T, f *schedulerFixture) {
	t.Helper()
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='go_consumer_control'") != 0 {
		t.Fatal("test control table already exists")
	}
	request := consumercontrol.Request{Generation: f.repo.Generation, Revision: 1, Origin: consumercontrol.LegacyTick}
	guard := func(req consumercontrol.Request) error {
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		return consumercontrol.GuardNewRun(f.ctx, tx, req)
	}
	if err := guard(request); !errors.Is(err, consumercontrol.ErrStorage) {
		t.Fatalf("missing control table must fail closed: %v", err)
	}
	t.Run("consumer_baseline_preflight", func(t *testing.T) {
		testConsumerBaselinePreflight(t, f.sub(t))
	})
	f.exec(`CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_control"); err != nil {
			t.Error("test control table cleanup failed")
		}
	})
	t.Run("consumer_schema_metadata", func(t *testing.T) {
		f := f.sub(t)
		snapshot, err := dbschema.Inspect(f.ctx, f.db)
		if err != nil {
			t.Fatal(err)
		}
		if err := dbschema.CheckConsumerOverlay(snapshot, dbschema.ConsumerOverlayVersion); err != nil {
			t.Fatal("valid overlay rejected", err)
		}
		f.exec("ALTER TABLE go_consumer_control ADD COLUMN ci_unexpected BIGINT NULL")
		snapshot, err = dbschema.Inspect(f.ctx, f.db)
		if err != nil {
			t.Fatal(err)
		}
		if err := dbschema.CheckConsumerOverlay(snapshot, dbschema.ConsumerOverlayVersion); err == nil {
			t.Fatal("unexpected column accepted")
		}
		f.exec("ALTER TABLE go_consumer_control DROP COLUMN ci_unexpected")
	})
	f.exec("INSERT INTO go_consumer_control(id,generation,revision,mode,owner_digest,lease_until) VALUES(1,?,1,'legacy',NULL,0)", f.repo.Generation)
	t.Run("consumer_candidate_preflight", func(t *testing.T) {
		testConsumerCandidatePreflight(t, f.sub(t))
	})
	t.Run("consumer_preflight_session_cleanup", func(t *testing.T) {
		testConsumerPreflightSessionCleanup(t, f.sub(t))
	})
	t.Run("consumer_restore_reset", func(t *testing.T) {
		testConsumerRestoreReset(t, f.sub(t))
	})
	t.Run("consumer_singleton_state", func(t *testing.T) {
		f := f.sub(t)
		check := func(generation string) error {
			t.Helper()
			tx, err := f.db.BeginTx(f.ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			return consumercontrol.GuardSingleton(f.ctx, tx, generation)
		}
		if err := check(f.repo.Generation); err != nil {
			t.Fatal("valid singleton rejected", err)
		}
		held, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := consumercontrol.GuardSingleton(f.ctx, held, f.repo.Generation); err != nil {
			held.Rollback()
			t.Fatal(err)
		}
		_, contender := schedulerDB(t)
		contender.SetMaxOpenConns(1)
		var heldID, contenderID int64
		if held.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&heldID) != nil || contender.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&contenderID) != nil || heldID == contenderID {
			held.Rollback()
			t.Fatal("two separate connections required")
		}
		blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
		_, err = contender.ExecContext(blocked, "UPDATE app_runtime_generation SET generation=generation WHERE id=1")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			held.Rollback()
			t.Fatal("generation lock not held", err)
		}
		if err := held.Rollback(); err != nil {
			t.Fatal(err)
		}
		f.exec("INSERT INTO go_consumer_control(id,generation,revision,mode,owner_digest,lease_until) VALUES(2,?,1,'legacy',NULL,0)", f.repo.Generation)
		if err := check(f.repo.Generation); !errors.Is(err, consumercontrol.ErrState) {
			t.Fatal("extra row accepted", err)
		}
		f.exec("DELETE FROM go_consumer_control WHERE id=2")
		f.exec("UPDATE go_consumer_control SET owner_digest=X'' WHERE id=1")
		if err := check(f.repo.Generation); !errors.Is(err, consumercontrol.ErrState) {
			t.Fatal("invalid digest accepted", err)
		}
		f.exec("UPDATE go_consumer_control SET owner_digest=NULL WHERE id=1")
		f.exec("UPDATE go_consumer_control SET mode='worker' WHERE id=1")
		if err := check(f.repo.Generation); !errors.Is(err, consumercontrol.ErrState) {
			t.Fatal("worker without owner accepted", err)
		}
		f.exec("UPDATE go_consumer_control SET mode='legacy' WHERE id=1")
		token, err := consumercontrol.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		digest, err := token.Digest()
		if err != nil {
			t.Fatal(err)
		}
		f.exec("UPDATE go_consumer_control SET mode='worker',owner_digest=?,lease_until=? WHERE id=1", digest[:], schedulerClock(t, f.db)+60000)
		if err := check(f.repo.Generation); err != nil {
			t.Fatal("valid worker rejected", err)
		}
		f.exec("UPDATE go_consumer_control SET mode='draining',revision=2 WHERE id=1")
		if err := check(f.repo.Generation); err != nil {
			t.Fatal("valid draining rejected", err)
		}
		f.exec("UPDATE go_consumer_control SET mode='legacy',revision=1,owner_digest=NULL,lease_until=0 WHERE id=1")
		other := "11111111-2222-4333-8444-555555555555"
		if other == f.repo.Generation {
			other = "11111111-2222-4333-8444-555555555556"
		}
		if err := check(other); !errors.Is(err, consumercontrol.ErrStale) {
			t.Fatal("different generation accepted", err)
		}
		if err := check(f.repo.Generation); err != nil {
			t.Fatal("valid singleton not restored", err)
		}
	})
	if err := guard(request); err != nil {
		t.Fatal("legacy guard", err)
	}
	drainTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	drained, err := consumercontrol.BeginDrain(f.ctx, drainTx, f.repo.Generation, 1)
	if err != nil || drained.Mode != consumercontrol.Draining || drained.Revision != 2 {
		drainTx.Rollback()
		t.Fatal("legacy drain", err)
	}
	drainingRequest := request
	drainingRequest.Revision = 2
	if err := consumercontrol.GuardNewRun(f.ctx, drainTx, drainingRequest); !errors.Is(err, consumercontrol.ErrDenied) {
		drainTx.Rollback()
		t.Fatal("draining transaction allowed new run", err)
	}
	if err := drainTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := guard(request); err != nil {
		t.Fatal("rollback did not restore legacy", err)
	}
	staleTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = consumercontrol.BeginDrain(f.ctx, staleTx, f.repo.Generation, 2)
	staleTx.Rollback()
	if !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("stale drain accepted", err)
	}
	wrongGeneration := request
	wrongGeneration.Generation = "11111111-2222-4333-8444-555555555555"
	if wrongGeneration.Generation == f.repo.Generation {
		wrongGeneration.Generation = "11111111-2222-4333-8444-555555555556"
	}
	if err := guard(wrongGeneration); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("stale generation accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET generation=? WHERE id=1", wrongGeneration.Generation)
	if err := guard(request); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("mismatched control generation accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET generation=? WHERE id=1", f.repo.Generation)
	stale := request
	stale.Revision = 2
	if err := guard(stale); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("stale revision accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='draining',revision=2 WHERE id=1")
	request.Revision = 2
	if err := guard(request); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("draining accepted a new run", err)
	}
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	now := schedulerClock(t, f.db)
	f.exec("UPDATE go_consumer_control SET mode='worker',revision=3,owner_digest=?,lease_until=? WHERE id=1", digest[:], now+60000)
	worker := consumercontrol.Request{Generation: f.repo.Generation, Revision: 3, Origin: consumercontrol.WorkerRun, Token: token}
	if err := guard(worker); err != nil {
		t.Fatal("worker guard", err)
	}
	t.Run("guarded_run", func(t *testing.T) {
		testGuardedRun(t, f.sub(t), worker)
	})
	t.Run("bound_attempt_lifecycle", func(t *testing.T) {
		testBoundAttemptLifecycle(t, f.sub(t), worker)
	})
	t.Run("bound_reservation_interrupt_race", func(t *testing.T) {
		testBoundReservationInterruptRace(t, f.sub(t), worker)
	})
	leaseTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumercontrol.GuardRunLease(f.ctx, leaseTx, worker, now+60000); err != nil {
		leaseTx.Rollback()
		t.Fatal("owner deadline should be accepted", err)
	}
	if err := consumercontrol.GuardRunLease(f.ctx, leaseTx, worker, now+60001); !errors.Is(err, consumercontrol.ErrDenied) {
		leaseTx.Rollback()
		t.Fatal("run outlived owner", err)
	}
	if err := consumercontrol.GuardClaimLease(f.ctx, leaseTx, worker, now+50000, now+50000); err != nil {
		leaseTx.Rollback()
		t.Fatal("bounded claim rejected", err)
	}
	if err := consumercontrol.GuardClaimLease(f.ctx, leaseTx, worker, now+50000, now+50001); !errors.Is(err, consumercontrol.ErrDenied) {
		leaseTx.Rollback()
		t.Fatal("job outlived run", err)
	}
	if _, err := leaseTx.ExecContext(f.ctx, "UPDATE go_consumer_control SET lease_until=1 WHERE id=1"); err != nil {
		leaseTx.Rollback()
		t.Fatal(err)
	}
	if err := consumercontrol.GuardRunLease(f.ctx, leaseTx, worker, now+60000); !errors.Is(err, consumercontrol.ErrDenied) {
		leaseTx.Rollback()
		t.Fatal("recheck accepted expired owner", err)
	}
	if err := consumercontrol.GuardClaimLease(f.ctx, leaseTx, worker, now+50000, now+40000); !errors.Is(err, consumercontrol.ErrDenied) {
		leaseTx.Rollback()
		t.Fatal("claim recheck accepted expired owner", err)
	}
	if err := leaseTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	t.Run("owner_renewal", func(t *testing.T) {
		testConsumerRenewal(t, f.sub(t), worker, now+60000)
	})
	workerDrainTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	workerDraining, err := consumercontrol.BeginDrain(f.ctx, workerDrainTx, f.repo.Generation, 3)
	if err != nil || workerDraining.OwnerDigest != digest || workerDraining.LeaseUntilMS != now+60000 {
		workerDrainTx.Rollback()
		t.Fatal("worker drain lost owner fence", err)
	}
	if err := consumercontrol.GuardCompletion(f.ctx, workerDrainTx, worker); err != nil {
		workerDrainTx.Rollback()
		t.Fatal("draining rejected existing consumer", err)
	}
	wrongOwner := worker
	wrongOwner.Token, err = consumercontrol.NewToken()
	if err != nil {
		workerDrainTx.Rollback()
		t.Fatal(err)
	}
	if err := consumercontrol.GuardCompletion(f.ctx, workerDrainTx, wrongOwner); !errors.Is(err, consumercontrol.ErrDenied) {
		workerDrainTx.Rollback()
		t.Fatal("draining accepted wrong consumer", err)
	}
	if _, err := workerDrainTx.ExecContext(f.ctx, "UPDATE go_consumer_control SET lease_until=1 WHERE id=1"); err != nil {
		workerDrainTx.Rollback()
		t.Fatal(err)
	}
	if err := consumercontrol.GuardCompletion(f.ctx, workerDrainTx, worker); !errors.Is(err, consumercontrol.ErrDenied) {
		workerDrainTx.Rollback()
		t.Fatal("completion guard accepted expired lease", err)
	}
	if err := workerDrainTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// Повторный guard в той же транзакции видит истёкший fence; rollback
	// сохраняет исходный срок, как должен поступать вызывающий при отказе.
	recheckTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumercontrol.GuardNewRun(f.ctx, recheckTx, worker); err != nil {
		recheckTx.Rollback()
		t.Fatal("initial worker guard", err)
	}
	if _, err := recheckTx.ExecContext(f.ctx, "UPDATE go_consumer_control SET lease_until=1 WHERE id=1"); err != nil {
		recheckTx.Rollback()
		t.Fatal(err)
	}
	if err := consumercontrol.GuardNewRun(f.ctx, recheckTx, worker); !errors.Is(err, consumercontrol.ErrDenied) {
		recheckTx.Rollback()
		t.Fatal("final guard accepted expired lease", err)
	}
	if err := recheckTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := guard(worker); err != nil {
		t.Fatal("rollback did not preserve worker lease", err)
	}
	other, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	worker.Token = other
	if err := guard(worker); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("wrong worker token accepted", err)
	}
	worker.Token = token
	f.exec("UPDATE go_consumer_control SET owner_digest=X'' WHERE id=1")
	if err := guard(worker); !errors.Is(err, consumercontrol.ErrState) {
		t.Fatal("empty non-null digest accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET owner_digest=X'00' WHERE id=1")
	if err := guard(worker); !errors.Is(err, consumercontrol.ErrState) {
		t.Fatal("malformed digest accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET owner_digest=? WHERE id=1", digest[:])
	f.exec("UPDATE go_consumer_control SET lease_until=1 WHERE id=1")
	if err := guard(worker); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("expired worker lease accepted", err)
	}
	// Пока вызывающая транзакция удерживает fence, другая не меняет режим.
	f.exec("UPDATE go_consumer_control SET mode='legacy',revision=4,owner_digest=NULL,lease_until=0 WHERE id=1")
	request.Revision = 4
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := consumercontrol.GuardNewRun(f.ctx, tx, request); err != nil {
		t.Fatal("locked guard", err)
	}
	blockedCtx, cancel := context.WithTimeout(f.ctx, 250*time.Millisecond)
	defer cancel()
	_, err = f.db.ExecContext(blockedCtx, "UPDATE go_consumer_control SET revision=5 WHERE id=1")
	if err == nil || blockedCtx.Err() == nil {
		t.Fatal("control row was not held until transaction end")
	}
	blockedGeneration, cancelGeneration := context.WithTimeout(f.ctx, 250*time.Millisecond)
	defer cancelGeneration()
	_, err = f.db.ExecContext(blockedGeneration, "UPDATE app_runtime_generation SET generation=generation WHERE id=1")
	if err == nil || blockedGeneration.Err() == nil {
		t.Fatal("generation row was not held until transaction end")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE go_consumer_control SET revision=5 WHERE id=1"); err != nil {
		t.Fatal("lock not released", err)
	}
	commitTx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := consumercontrol.BeginDrain(f.ctx, commitTx, f.repo.Generation, 5)
	if err != nil || committed.Revision != 6 {
		commitTx.Rollback()
		t.Fatal("drain before commit", err)
	}
	competitorStarted := make(chan struct{})
	competitorResult := make(chan error, 1)
	go func() {
		otherTx, beginErr := f.db.BeginTx(f.ctx, nil)
		if beginErr != nil {
			close(competitorStarted)
			competitorResult <- beginErr
			return
		}
		defer otherTx.Rollback()
		close(competitorStarted)
		_, transitionErr := consumercontrol.BeginDrain(f.ctx, otherTx, f.repo.Generation, 5)
		competitorResult <- transitionErr
	}()
	<-competitorStarted
	if err := commitTx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-competitorResult:
		if !errors.Is(err, consumercontrol.ErrStale) {
			t.Fatal("competing drain did not fail stale", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("competing drain did not complete")
	}
	request.Revision = 6
	if err := guard(request); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("committed drain did not block new run", err)
	}
}
