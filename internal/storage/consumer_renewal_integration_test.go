//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
)

// Вызывается только внутри fixture с проверенной одноразовой MariaDB.
func testConsumerRenewal(t *testing.T, f *schedulerFixture, req consumercontrol.Request, originalUntil int64) {
	t.Helper()
	newUntil := schedulerClock(t, f.db) + 120000
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	next, err := consumercontrol.RenewOwner(f.ctx, tx, req, newUntil)
	if err != nil || next.Revision != req.Revision || next.LeaseUntilMS != newUntil {
		t.Fatal("renewal failed", err)
	}
	if err := consumercontrol.GuardNewRun(f.ctx, tx, req); err != nil {
		t.Fatal("renewal invalidated original request", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := f.db.QueryRowContext(f.ctx, "SELECT lease_until FROM go_consumer_control WHERE id=1").Scan(&stored); err != nil || stored != originalUntil {
		t.Fatal("rollback changed lease", err)
	}
	// Продление держит ту же generation-блокировку, что переход в draining.
	tx, err = f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := consumercontrol.RenewOwner(f.ctx, tx, req, newUntil); err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	other, err := f.db.BeginTx(blockedCtx, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_, err = consumercontrol.BeginDrain(blockedCtx, other, req.Generation, req.Revision)
	other.Rollback()
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("drain bypassed renewal lock", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer f.exec("UPDATE go_consumer_control SET lease_until=? WHERE id=1", originalUntil)
	if err := f.db.QueryRowContext(f.ctx, "SELECT lease_until FROM go_consumer_control WHERE id=1").Scan(&stored); err != nil || stored != newUntil {
		t.Fatal("commit did not persist lease", err)
	}
	check := func(request consumercontrol.Request, until int64, want error) {
		t.Helper()
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_, err = consumercontrol.RenewOwner(f.ctx, tx, request, until)
		if !errors.Is(err, want) {
			t.Fatalf("renewal got %v, want %v", err, want)
		}
	}
	check(req, newUntil, consumercontrol.ErrDenied)
	check(req, schedulerClock(t, f.db)+consumercontrol.MaxOwnerLeaseMS+60000, consumercontrol.ErrDenied)
	wrong := req
	wrong.Token, err = consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	check(wrong, newUntil+1000, consumercontrol.ErrDenied)
	wrong = req
	wrong.Revision++
	check(wrong, newUntil+1000, consumercontrol.ErrStale)
	// После draining даже действующий token с новой revision не продлевается.
	tx, err = f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	drained, err := consumercontrol.BeginDrain(f.ctx, tx, req.Generation, req.Revision)
	if err != nil {
		t.Fatal(err)
	}
	wrong = req
	wrong.Revision = drained.Revision
	if _, err := consumercontrol.RenewOwner(f.ctx, tx, wrong, newUntil+1000); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("draining owner renewed", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE go_consumer_control SET lease_until=1 WHERE id=1")
	check(req, newUntil+1000, consumercontrol.ErrDenied)
}
