//go:build mariadb && v39restore && historicalrestore && unsignedrestore

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"itmap/internal/backup"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMariaDBUnsignedPreserveRaceDisposable(t *testing.T) {
	for _, version := range []int{14, 15} {
		for _, table := range []string{"google_calendar_connection", "vacancy_audit_runs"} {
			for _, first := range []string{"writer", "restore"} {
				t.Run(fmt.Sprint(version)+" "+table+" "+first, func(t *testing.T) {
					f, v := openUnsignedFixture(t, version, "omitted")
					old := f.generation()
					query, prefix, suffix := "UPDATE google_calendar_connection SET account_email='CI concurrent preserved' WHERE id=1", "UPDATE google_calendar_connection SET", ""
					if table == "vacancy_audit_runs" {
						query = "INSERT INTO vacancy_audit_runs(run_id,deleted_count,publication_date_update_count,completed_at) VALUES('CI concurrent audit',1,2,'2026-10-28')"
						prefix = "INSERT INTO vacancy_audit_runs"
					}
					writePattern := prefix + "%"
					readPrefix := "SELECT `id`,`account_email`"
					if table == "vacancy_audit_runs" {
						readPrefix = "SELECT `run_id`,`deleted_count`"
					}
					if first == "restore" {
						prefix, suffix = readPrefix, " FOR UPDATE"
					}
					pool, hook := preservePool(f, prefix, suffix)
					repo, writer := backup.Repository{DB: f.db}, f.db
					if first == "restore" {
						repo.DB = pool
					} else {
						writer = pool
					}
					ctx, cancel := context.WithCancel(f.ctx)
					var actors sync.WaitGroup
					restored, written := make(chan error, 1), make(chan error, 1)
					// Join both SQL actors before the barrier pool and fixture cleanups run.
					t.Cleanup(func() {
						hook.unblock()
						cancel()
						done := make(chan struct{})
						go func() { actors.Wait(); close(done) }()
						select {
						case <-done:
						case <-time.After(8 * time.Second):
							t.Error("unsigned preserve actors did not stop")
						}
					})
					startRestore := func() {
						actors.Add(1)
						go func() { defer actors.Done(); _, e := repo.Restore(ctx, []byte(v.Body)); restored <- e }()
					}
					startWrite := func() {
						actors.Add(1)
						go func() { defer actors.Done(); written <- unsignedPreservedWrite(ctx, writer, query) }()
					}
					if first == "writer" {
						startWrite()
					} else {
						startRestore()
					}
					select {
					case <-hook.entered:
					case <-ctx.Done():
						t.Fatal("unsigned preserve barrier not reached")
					}
					if first == "writer" {
						startRestore()
						waitPreserveBlocked(f, readPrefix+"%FOR UPDATE")
					} else {
						startWrite()
						waitPreserveBlocked(f, writePattern)
					}
					select {
					case <-restored:
						t.Fatal("restore crossed held preserved row/range")
					default:
					}
					select {
					case <-written:
						t.Fatal("writer crossed held preserved row/range")
					default:
					}
					hook.unblock()
					restoreErr, writeErr := <-restored, <-written
					if restoreErr != nil || writeErr != nil {
						t.Fatalf("unsigned preserve race failed: restore=%v writer=%v", restoreErr, writeErr)
					}
					for i := range v.Expected {
						tab := &v.Expected[i]
						if tab.Name != table {
							continue
						}
						if table == "google_calendar_connection" {
							for _, row := range tab.Rows {
								if string(row["id"]) == "1" {
									row["account_email"] = json.RawMessage(`"CI concurrent preserved"`)
								}
							}
						} else {
							tab.Rows = append(tab.Rows, map[string]json.RawMessage{"run_id": json.RawMessage(`"CI concurrent audit"`), "deleted_count": json.RawMessage("1"), "publication_date_update_count": json.RawMessage("2"), "completed_at": json.RawMessage(`"2026-10-28"`)})
						}
					}
					if f.generation() == old || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
						t.Fatal("unsigned restore lost concurrent calendar/audit write or changed other source rows")
					}
				})
			}
		}
	}
}
func unsignedPreservedWrite(ctx context.Context, db *sql.DB, q string) error {
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, q); e != nil {
		return e
	}
	return tx.Commit()
}
