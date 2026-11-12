//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
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
	f.exec(`CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_control"); err != nil {
			t.Error("test control table cleanup failed")
		}
	})
	f.exec("INSERT INTO go_consumer_control(id,generation,revision,mode,owner_digest,lease_until) VALUES(1,?,1,'legacy',NULL,0)", f.repo.Generation)
	if err := guard(request); err != nil {
		t.Fatal("legacy guard", err)
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
	worker := consumercontrol.Request{Generation: f.repo.Generation, Revision: 3, Origin: consumercontrol.WorkerRun, Token: token, PilotUntilMS: now + 120000}
	if err := guard(worker); err != nil {
		t.Fatal("worker guard", err)
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
}
