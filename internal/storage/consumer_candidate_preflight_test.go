//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumerpreflight"
)

func testConsumerCandidatePreflight(t *testing.T, f *schedulerFixture) {
	t.Helper()
	if err := consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); err != nil {
		t.Fatal("candidate rejected", err)
	}
	if err := consumerpreflight.CheckBaseline(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("candidate accepted as baseline", err)
	}
	if f.count("SELECT COUNT(*) FROM go_consumer_control WHERE id=1 AND revision=1 AND mode='legacy' AND owner_digest IS NULL AND lease_until=0") != 1 {
		t.Fatal("read-only preflight changed state")
	}
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	if err := consumerpreflight.CheckCandidate(f.ctx, f.db, other); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("stale generation accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET revision=0 WHERE id=1")
	if err := consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("invalid row accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET revision=1 WHERE id=1")
	f.exec("ALTER TABLE go_consumer_control ADD COLUMN ci_unknown BIGINT NULL")
	if err := consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("unknown metadata accepted", err)
	}
	f.exec("ALTER TABLE go_consumer_control DROP COLUMN ci_unknown")
	// Проверить конкурентный advisory lock на выделенном соединении; проверка
	// не должна блокироваться, менять данные или освобождать чужой lock.
	blocker, err := f.db.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	var database string
	if err = blocker.QueryRowContext(f.ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(database))
	key := "itmap.restore.v39:" + hex.EncodeToString(h[:16])
	var acquired sql.NullInt64
	if err = blocker.QueryRowContext(f.ctx, "SELECT GET_LOCK(?,0)", key).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		t.Fatal("cannot acquire test lock", err)
	}
	owned := true
	defer func() {
		if owned {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var released sql.NullInt64
			if err := blocker.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
				t.Error("cannot release test lock", err)
			}
		}
	}()
	if err = consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrBusy) {
		t.Fatal("shared schema lock not respected", err)
	}
	var owner sql.NullInt64
	if err = blocker.QueryRowContext(f.ctx, "SELECT IS_USED_LOCK(?)", key).Scan(&owner); err != nil || !owner.Valid {
		t.Fatal("foreign lock was released", err)
	}
	var released sql.NullInt64
	if err = blocker.QueryRowContext(f.ctx, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
		t.Fatal("release test lock", err)
	}
	owned = false
	if err = consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); err != nil {
		t.Fatal("preflight failed after lock release", err)
	}
}

func testConsumerBaselinePreflight(t *testing.T, f *schedulerFixture) {
	t.Helper()
	if err := consumerpreflight.CheckBaseline(f.ctx, f.db, f.repo.Generation); err != nil {
		t.Fatal("baseline rejected", err)
	}
	if err := consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("baseline accepted as candidate", err)
	}
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	if err := consumerpreflight.CheckBaseline(f.ctx, f.db, other); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("stale generation accepted", err)
	}
	f.exec("CREATE TABLE ci_unknown_preflight (id BIGINT NOT NULL PRIMARY KEY)")
	defer f.exec("DROP TABLE ci_unknown_preflight")
	if err := consumerpreflight.CheckBaseline(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("unknown table accepted", err)
	}
}

// В этих сценариях испорченное соединение не должно вернуться в пул.
func testConsumerPreflightSessionCleanup(t *testing.T, f *schedulerFixture) {
	t.Helper()
	_, db := schedulerDB(t)
	db.SetMaxOpenConns(1)
	poison := func(sqlText string, args ...any) int64 {
		t.Helper()
		conn, err := db.Conn(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err = conn.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(f.ctx, sqlText, args...); err != nil {
			t.Fatal(err)
		}
		if err = conn.Close(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	verifyDiscarded := func(oldID int64) {
		t.Helper()
		var id int64
		var autocommit int
		if err := db.QueryRowContext(f.ctx, "SELECT CONNECTION_ID(), @@autocommit").Scan(&id, &autocommit); err != nil || id == oldID || autocommit != 1 {
			t.Fatal("contaminated session returned to pool", err)
		}
	}
	old := poison("SET SESSION autocommit=0")
	if err := consumerpreflight.CheckCandidate(f.ctx, db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSession) {
		t.Fatal("non-autocommit session accepted", err)
	}
	verifyDiscarded(old)
	var database string
	if err := db.QueryRowContext(f.ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(database))
	key := "itmap.restore.v39:" + hex.EncodeToString(h[:16])
	old = poison("DO GET_LOCK(?,0)", key)
	var owner sql.NullInt64
	if err := db.QueryRowContext(f.ctx, "SELECT IS_USED_LOCK(?)", key).Scan(&owner); err != nil || !owner.Valid || owner.Int64 != old {
		t.Fatal("test did not acquire lock", err)
	}
	if err := consumerpreflight.CheckCandidate(f.ctx, db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSession) {
		t.Fatal("reentrant session accepted", err)
	}
	verifyDiscarded(old)
	if err := db.QueryRowContext(f.ctx, "SELECT IS_USED_LOCK(?)", key).Scan(&owner); err != nil || owner.Valid {
		t.Fatal("discard did not release leaked lock", err)
	}
	if err := consumerpreflight.CheckCandidate(f.ctx, db, f.repo.Generation); err != nil {
		t.Fatal("clean session rejected", err)
	}
}
