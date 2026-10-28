//go:build mariadb && v39restore && historicalrestore && unsignedrestore

package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"itmap/internal/backup"
	"os"
	"os/exec"
	"reflect"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func openUnsignedFixture(t *testing.T, version int, audit string) (*restoreFixture, historicalFrame) {
	t.Helper()
	f := openRestoreFixture(t)
	out, e := exec.CommandContext(f.ctx, "node", "../../scripts/check-historical-sql.mjs", "--version", fmt.Sprint(version), "--audit", audit, "--stdio").Output()
	var v historicalFrame
	if e != nil || json.Unmarshal(out, &v) != nil {
		t.Fatal("unsigned source SQL oracle failed")
	}
	f.restore([]byte(v.Initial))
	return f, v
}
func unsignedReader(f *restoreFixture) *sql.DB {
	f.t.Helper()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		f.t.Fatal("unsafe unsigned reader target")
	}
	cfg.User = "itmap_readonly_ci"
	cfg.Passwd = "ci_only_disposable_reader"
	pool, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		f.t.Fatal(e)
	}
	pool.SetMaxOpenConns(1)
	f.t.Cleanup(func() { pool.Close() })
	return pool
}
func unsignedNext(f *restoreFixture) uint64 {
	f.t.Helper()
	var next uint64
	if e := f.db.QueryRowContext(f.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&next); e != nil {
		f.t.Fatal("cannot read unsigned counter")
	}
	return next
}
func TestMariaDBUnsignedRestoreDisposable(t *testing.T) {
	for _, version := range []int{14, 15} {
		for _, audit := range []string{"omitted", "empty", "replace"} {
			t.Run(fmt.Sprint(version)+" "+audit, func(t *testing.T) {
				f, v := openUnsignedFixture(t, version, audit)
				repo := backup.Repository{DB: f.db}
				before, gen, next := f.snapshot(), f.generation(), unsignedNext(f)
				// Real SELECT-only account proves /validate performs no counter/schema writes.
				validation, e := (backup.Repository{DB: unsignedReader(f)}).ValidateRestore(f.ctx, []byte(v.Body))
				if e != nil || validation.Rows != v.Rows || validation.Tables != 52 || !reflect.DeepEqual(validation.Warnings, v.Warnings) {
					t.Fatalf("unsigned read-only validate differs: %v", e)
				}
				if gen != f.generation() || next != unsignedNext(f) || !reflect.DeepEqual(before, f.snapshot()) {
					t.Fatal("unsigned validation changed state")
				}
				result, e := repo.Restore(f.ctx, []byte(v.Body))
				if e != nil {
					t.Fatalf("unsigned restore failed: %v", e)
				}
				if result.SourceVersion != version || result.Generation == gen || result.Generation != f.generation() || result.Rows != v.Rows || !reflect.DeepEqual(result.Warnings, v.Warnings) {
					t.Fatal("unsigned response/generation differs")
				}
				if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
					t.Fatal("unsigned MariaDB rows differ from unchanged source SQL")
				}
				if unsignedNext(f) != next || f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='CI_untrusted_metadata'") != 0 {
					t.Fatal("unsigned file metadata was trusted")
				}
				exported := f.download()
				cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-v39-export.mjs")
				cmd.Stdin = bytes.NewReader(exported)
				if e = cmd.Run(); e != nil {
					t.Fatal("source rejected unsigned→v39 export")
				}
				again, e := repo.Restore(f.ctx, exported)
				if e != nil || again.SourceVersion != 39 || again.Generation == result.Generation || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
					t.Fatal("unsigned→v39 roundtrip differs")
				}
			})
		}
	}
	for _, stage := range []string{"DELETE FROM `company_vacancies`", "INSERT INTO `google_calendar_connection`", "UPDATE app_runtime_generation SET"} {
		t.Run("late rollback "+stage, func(t *testing.T) {
			f, v := openUnsignedFixture(t, 14, "omitted")
			before, gen := f.snapshot(), f.generation()
			hook := &restoreFault{prefix: stage}
			pool := restoreFaultPool(f, hook)
			if _, e := (backup.Repository{DB: pool}).Restore(f.ctx, []byte(v.Body)); e == nil || !hook.fired {
				t.Fatal("unsigned late fault did not run/fail")
			}
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("unsigned fault committed partial rows/generation")
			}
			if _, e := (backup.Repository{DB: f.db}).Restore(f.ctx, []byte(v.Body)); e != nil || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("unsigned fresh retry differs")
			}
		})
	}
}
