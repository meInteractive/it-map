//go:build mariadb && v39restore && restorehttp

package storage

import (
	"database/sql"
	"errors"
	"itmap/internal/backup"
	"reflect"
	"testing"
)

func TestMariaDBV39RestoreLateResultDisposable(t *testing.T) {
	t.Run("late synthetic generated result cannot cross restore generation", func(t *testing.T) {
		f := openRestoreFixture(t)
		f.restore(f.body)
		captured := f.generation()
		// External work is represented by a channel, with zero network/provider/key.
		ready := make(chan struct{})
		writeErr := make(chan error, 1)
		called := false
		go func() {
			<-ready
			writeErr <- WithinGenerationTx(f.ctx, f.db, captured, func(tx *sql.Tx) error {
				called = true
				_, e := tx.ExecContext(f.ctx, "UPDATE vacancy_cover_letters SET markdown='CI stale generated letter' WHERE user_name='Nastya' AND vacancy_id=2")
				return e
			})
		}()
		f.restore(f.body)
		close(ready)
		if e := <-writeErr; !errors.Is(e, ErrGenerationChanged) || called {
			t.Fatal("old generated result entered write callback")
		}
		if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
			t.Fatal("late result changed restored values")
		}
		current := f.generation()
		if e := WithinGenerationTx(f.ctx, f.db, current, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(f.ctx, "UPDATE vacancy_cover_letters SET markdown='CI fresh generated letter' WHERE user_name='Nastya' AND vacancy_id=2")
			return e
		}); e != nil {
			t.Fatal("fresh generated result rejected")
		}
		if f.count("SELECT COUNT(*) FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=2 AND markdown='CI fresh generated letter'") != 1 {
			t.Fatal("fresh result did not commit")
		}
		before := f.snapshot()
		fault := errors.New("CI generated result failure")
		if e := WithinGenerationTx(f.ctx, f.db, current, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(f.ctx, "UPDATE vacancy_cover_letters SET markdown='CI partial result' WHERE user_name='Nastya' AND vacancy_id=2"); e != nil {
				return e
			}
			return fault
		}); !errors.Is(e, fault) {
			t.Fatal("generation callback failure hidden")
		}
		if current != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
			t.Fatal("generation callback failure did not roll back")
		}
	})
	t.Run("result generation lock serializes restore before callback statements", func(t *testing.T) {
		f := openRestoreFixture(t)
		f.restore(f.body)
		captured := f.generation()
		pool, hook := restoreBarrierPool(f)
		written := make(chan error, 1)
		restored := make(chan error, 1)
		go func() {
			written <- WithinGenerationTx(f.ctx, pool, captured, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(f.ctx, "UPDATE vacancy_cover_letters SET markdown='CI pre-restore result' WHERE user_name='Nastya' AND vacancy_id=2")
				return e
			})
		}()
		waitRestoreBarrier(f, hook)
		go func() { _, e := (backup.Repository{DB: f.db}).RestoreV39(f.ctx, f.body); restored <- e }()
		waitGenerationWaiter(f)
		select {
		case <-restored:
			t.Fatal("restore crossed result generation lock")
		default:
		}
		hook.unblock()
		if e := <-written; e != nil {
			t.Fatalf("result before restore failed: %T", e)
		}
		if e := <-restored; e != nil {
			t.Fatalf("restore after result failed: %T", e)
		}
		if captured == f.generation() || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
			t.Fatal("pre-restore result survived replacement")
		}
	})
}

// The source wrapper's atomic generation check is the oracle for this boundary;
// actual AI endpoint orchestration, provider success/errors and leases retain
// their separate migration gates and are not claimed by these synthetic tests.
