//go:build mariadb && profilewrites && v39exports && restorecounters

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"itmap/internal/backup"
)

func counterFile(f *profileSettingsFixture, seq int64) []byte {
	f.t.Helper()
	file, e := (backup.Repository{DB: f.db}).Export(f.ctx)
	if e != nil {
		f.t.Fatal("cannot build synthetic counter input")
	}
	var p map[string]json.RawMessage
	_ = json.Unmarshal(file.Bytes, &p)
	var metadata map[string]json.RawMessage
	_ = json.Unmarshal(p["metadata"], &metadata)
	metadata["sequences"], _ = json.Marshal([]backup.Sequence{{Name: "agency_identity", Seq: float64(seq)}})
	p["metadata"], _ = json.Marshal(metadata)
	raw, _ := json.Marshal(p)
	output, e := backup.EncodeV39(raw)
	if e != nil {
		f.t.Fatal("cannot sign synthetic counter input")
	}
	return output
}
func counterNext(f *profileSettingsFixture) int64 {
	f.t.Helper()
	var n int64
	if e := f.db.QueryRowContext(f.ctx, `SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='agency_identity'`).Scan(&n); e != nil {
		f.t.Fatal("cannot inspect synthetic counter")
	}
	return n
}
func counterSnapshot(f *profileSettingsFixture) []backup.Table {
	f.t.Helper()
	file, e := (backup.Repository{DB: f.db}).Export(f.ctx)
	if e != nil {
		f.t.Fatal("cannot inspect original counter fixture")
	}
	return exportPayload(f.t, file).Storage.Tables
}

func seedCounterRecords(f *profileSettingsFixture) {
	f.t.Helper()
	f.save("Demo", "  CI counter preserved resume 😀  ", 0)
	result, e := f.db.ExecContext(f.ctx, `INSERT INTO agency_identity(agency_name) VALUES('  CI counter preserved agency 😀  ')`)
	if e != nil {
		f.t.Fatal("cannot seed nonempty counter fixture")
	}
	id, e := result.LastInsertId()
	if e != nil {
		f.t.Fatal(e)
	}
	f.t.Cleanup(func() {
		if _, e := f.db.ExecContext(context.Background(), `DELETE FROM agency_identity WHERE id=? AND agency_name='  CI counter preserved agency 😀  '`, id); e != nil {
			f.t.Error("cannot clean nonempty counter fixture")
		}
	})
}
func TestMariaDBV39RestoreCountersDisposable(t *testing.T) {
	t.Run("real ALTER retains write fence until explicit unlock", testCounterDDLRetainsFence)
	t.Run("error after actual table lock releases session and table", func(t *testing.T) { testCounterLateFault(t, "LOCK TABLES `agency_identity`") })
	t.Run("error after actual DDL leaves only safe gaps", func(t *testing.T) { testCounterLateFault(t, "ALTER TABLE `agency_identity`") })
	t.Run("cancel waiting for real SQL table lock releases ownership", testCounterCancelWait)
	t.Run("raise only preserves rows and generation with exclusive session ownership", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		seedCounterRecords(f)
		before := counterSnapshot(f)
		target := counterNext(f) + 10
		repo := backup.Repository{DB: f.db}
		session, e := repo.OpenV39RestoreSession(f.ctx, counterFile(f, target))
		if e != nil {
			t.Fatalf("counter preflight failed: %T", e)
		}
		defer session.Close()
		if counterNext(f) < target+1 || !reflect.DeepEqual(before, counterSnapshot(f)) {
			t.Fatal("counter preflight lowered high-water or changed rows")
		}
		if other, e := repo.OpenV39RestoreSession(f.ctx, counterFile(f, target-5)); e == nil {
			other.Close()
			t.Fatal("two restore sessions claimed one database")
		}
		if e = session.Close(); e != nil {
			t.Fatal("cannot release restore session")
		}
		lower, e := repo.OpenV39RestoreSession(f.ctx, counterFile(f, target-5))
		if e != nil {
			t.Fatal("fresh restore session failed")
		}
		lower.Close()
		if counterNext(f) < target+1 {
			t.Fatal("lower incoming counter caused reuse")
		}
	})
	t.Run("cancelled session releases ownership", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		ctx, cancel := context.WithCancel(f.ctx)
		session, e := (backup.Repository{DB: f.db}).OpenV39RestoreSession(ctx, counterFile(f, counterNext(f)))
		if e != nil {
			cancel()
			t.Fatal("cannot claim cancel fixture")
		}
		defer session.Close()
		cancel()
		timeout := time.NewTimer(3 * time.Second)
		defer timeout.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			var owner sql.NullInt64
			if e = f.db.QueryRowContext(f.ctx, `SELECT IS_USED_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)))`).Scan(&owner); e != nil {
				t.Fatal("cannot inspect cancelled restore ownership")
			}
			if !owner.Valid {
				break
			}
			select {
			case <-timeout.C:
				t.Fatal("cancel did not release restore ownership without Close")
			case <-tick.C:
			}
		}
		if e = session.Close(); e != nil {
			t.Fatal("cancel leaked restore session")
		}
	})
	t.Run("highest v39 sequence remains exact without decreasing counters", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		const max = int64(9007199254740991)
		session, e := (backup.Repository{DB: f.db}).OpenV39RestoreSession(f.ctx, counterFile(f, max))
		if e != nil {
			t.Fatalf("maximum safe counter failed: %T", e)
		}
		defer session.Close()
		if counterNext(f) != max+1 {
			t.Fatal("maximum v39 high-water lost precision")
		}
	})
}
