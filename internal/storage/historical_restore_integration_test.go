//go:build mariadb && v39restore && historicalrestore

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"itmap/internal/backup"
	"os/exec"
	"reflect"
	"testing"
)

type historicalFrame struct {
	Initial, Body string
	Expected      []backup.Table
	Warnings      []string
	Rows, Version int
}

func openHistoricalFixture(t *testing.T, version int) (*restoreFixture, historicalFrame) {
	t.Helper()
	f := openRestoreFixture(t)
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-historical-sql.mjs", "--version", fmt.Sprint(version), "--stdio")
	out, e := cmd.Output()
	var v historicalFrame
	if e != nil || json.Unmarshal(out, &v) != nil {
		t.Fatal("historical synthetic source SQL oracle failed")
	}
	f.restore([]byte(v.Initial))
	return f, v
}
func TestMariaDBHistoricalRestoreDisposable(t *testing.T) {
	for version := 16; version <= 38; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f, v := openHistoricalFixture(t, version)
			repo := backup.Repository{DB: f.db}
			before, gen := f.snapshot(), f.generation()
			var next uint64
			if e := f.db.QueryRowContext(f.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&next); e != nil {
				t.Fatal("counter read failed")
			}
			validation, e := repo.ValidateRestore(f.ctx, []byte(v.Body))
			if e != nil || validation.Rows != v.Rows || validation.Tables != 52 || !reflect.DeepEqual(validation.Warnings, v.Warnings) {
				t.Fatalf("historical validate mismatch: %v", e)
			}
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("historical validation changed rows/generation")
			}
			var after uint64
			if e = f.db.QueryRowContext(f.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&after); e != nil || next != after {
				t.Fatal("validation changed counter")
			}
			result, e := repo.Restore(f.ctx, []byte(v.Body))
			if e != nil {
				t.Fatalf("v%d restore failed: %v", version, e)
			}
			if result.SourceVersion != version || result.Generation == gen || result.Generation != f.generation() || result.Rows != v.Rows || !reflect.DeepEqual(result.Warnings, v.Warnings) {
				t.Fatal("historical summary/generation mismatch")
			}
			if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("historical SQL rows differ from unchanged source restore")
			}
			exported := f.download()
			cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-v39-export.mjs")
			cmd.Stdin = bytes.NewReader(exported)
			if e = cmd.Run(); e != nil {
				t.Fatal("legacy rejected historical SQL re-export")
			}
			again, e := repo.Restore(f.ctx, exported)
			if e != nil || again.SourceVersion != 39 || again.Generation == result.Generation || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("historical→v39→restore roundtrip mismatch")
			}
			if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies' AND AUTO_INCREMENT>=1000000") != 1 {
				t.Fatal("historical restore lowered source high-water")
			}
		})
	}
	for _, stage := range []string{"DELETE FROM `company_vacancies`", "INSERT INTO `user_profiles`", "UPDATE app_runtime_generation SET"} {
		t.Run("late rollback "+stage, func(t *testing.T) {
			f, v := openHistoricalFixture(t, 16)
			before, gen := f.snapshot(), f.generation()
			hook := &restoreFault{prefix: stage}
			pool := restoreFaultPool(f, hook)
			if _, e := (backup.Repository{DB: pool}).Restore(f.ctx, []byte(v.Body)); e == nil || !hook.fired {
				t.Fatal("historical late fault did not fail after real SQL")
			}
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("historical failure committed partial preserve or generation")
			}
			if _, e := (backup.Repository{DB: f.db}).Restore(f.ctx, []byte(v.Body)); e != nil || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("historical fresh retry failed")
			}
		})
	}
}
