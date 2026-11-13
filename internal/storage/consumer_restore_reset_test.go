//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
)

// Только disposable CI: смена generation здесь является синтетическим тестом,
// а не вызовом production restore.
func testConsumerRestoreReset(t *testing.T, f *schedulerFixture) {
	t.Helper()
	var oldUpdatedAt string
	if err := f.db.QueryRowContext(f.ctx, "SELECT updated_at FROM app_runtime_generation WHERE id=1").Scan(&oldUpdatedAt); err != nil {
		t.Fatal("cannot inspect fixture timestamp")
	}
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker',revision=7,owner_digest=?,lease_until=? WHERE id=1", digest[:], schedulerClock(t, f.db)+60000)
	// Вернуть обоим singleton исходную CI generation даже при ошибке теста.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Error("cannot restore fixture")
			return
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "UPDATE app_runtime_generation SET generation=?,updated_at=? WHERE id=1", f.repo.Generation, oldUpdatedAt); err != nil {
			t.Error("cannot restore generation")
			return
		}
		if _, err = tx.ExecContext(ctx, "UPDATE go_consumer_control SET generation=?,revision=1,mode='legacy',owner_digest=NULL,lease_until=0 WHERE id=1", f.repo.Generation); err != nil {
			t.Error("cannot restore singleton")
			return
		}
		if err = tx.Commit(); err != nil {
			t.Error("cannot commit fixture cleanup")
		}
	})
	oldRequest := consumercontrol.Request{Generation: f.repo.Generation, Revision: 7, Origin: consumercontrol.WorkerRun, Token: token}
	assertOld := func() {
		t.Helper()
		if f.count("SELECT COUNT(*) FROM go_consumer_control WHERE id=1 AND generation=? AND revision=7 AND mode='worker' AND owner_digest=?", f.repo.Generation, digest[:]) != 1 {
			t.Fatal("worker state changed after rollback")
		}
		if f.count("SELECT COUNT(*) FROM app_runtime_generation WHERE id=1 AND generation=?", f.repo.Generation) != 1 {
			t.Fatal("generation changed after rollback")
		}
	}
	assertOld()
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = consumercontrol.ResetForRestore(f.ctx, tx, other); !errors.Is(err, consumercontrol.ErrStale) {
		tx.Rollback()
		t.Fatal("stale generation accepted", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertOld()
	tx, err = f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	newGeneration, err := consumercontrol.ResetForRestore(f.ctx, tx, f.repo.Generation)
	if err != nil {
		tx.Rollback()
		t.Fatal("reset rejected", err)
	}
	if newGeneration == f.repo.Generation {
		tx.Rollback()
		t.Fatal("generation not rotated")
	}
	if err = consumercontrol.GuardNewRun(f.ctx, tx, oldRequest); !errors.Is(err, consumercontrol.ErrStale) {
		tx.Rollback()
		t.Fatal("old owner accepted", err)
	}
	newRequest := oldRequest
	newRequest.Generation = newGeneration
	newRequest.Revision = 1
	if err = consumercontrol.GuardNewRun(f.ctx, tx, newRequest); !errors.Is(err, consumercontrol.ErrDenied) {
		tx.Rollback()
		t.Fatal("draining accepted new work", err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, err = f.db.ExecContext(blocked, "UPDATE app_runtime_generation SET generation=generation WHERE id=1")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		tx.Rollback()
		t.Fatal("restore generation lock not held", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertOld()
	t.Run("failure after generation update rolls back both", func(t *testing.T) {
		f := f.sub(t)
		f.exec("CREATE TRIGGER ci_restore_reset_fail BEFORE UPDATE ON go_consumer_control FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='CI reset fault'")
		t.Cleanup(func() { f.exec("DROP TRIGGER ci_restore_reset_fail") })
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = consumercontrol.ResetForRestore(f.ctx, tx, f.repo.Generation); err == nil {
			tx.Rollback()
			t.Fatal("injected failure accepted")
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		assertOld()
	})
	tx, err = f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	newGeneration, err = consumercontrol.ResetForRestore(f.ctx, tx, f.repo.Generation)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal("synthetic commit failed", err)
	}
	if f.count("SELECT COUNT(*) FROM app_runtime_generation WHERE id=1 AND generation=?", newGeneration) != 1 || f.count("SELECT COUNT(*) FROM go_consumer_control WHERE id=1 AND generation=? AND revision=1 AND mode='draining' AND owner_digest IS NULL AND lease_until=0", newGeneration) != 1 {
		t.Fatal("reset state not committed together")
	}
	tx, err = f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = consumercontrol.GuardNewRun(f.ctx, tx, oldRequest); !errors.Is(err, consumercontrol.ErrStale) {
		tx.Rollback()
		t.Fatal("old owner accepted after commit", err)
	}
	newRequest.Generation = newGeneration
	if err = consumercontrol.GuardNewRun(f.ctx, tx, newRequest); !errors.Is(err, consumercontrol.ErrDenied) {
		tx.Rollback()
		t.Fatal("draining accepted after commit", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}
