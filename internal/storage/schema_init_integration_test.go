//go:build mariadb && v39restore && schemainit

package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/backup"
	"itmap/internal/dbschema"
	"os/exec"
	"reflect"
	"testing"
)

func TestMariaDBSchemaInitDisposable(t *testing.T) {
	for _, version := range []int{14, 16, 39} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx, db := openBlankSchemaFixture(t)
			f := &restoreFixture{t: t, ctx: ctx, db: db}
			result, e := dbschema.Initialize(ctx, db)
			if e != nil || !result.Ready || !result.Seeded || result.Applied != 217 {
				t.Fatalf("fresh trusted schema init failed: %+v %v", result, e)
			}
			initial := f.generation()
			before := f.snapshot()
			again, e := dbschema.Initialize(ctx, db)
			if e != nil || !again.Ready || again.Applied != 0 || again.Seeded || initial != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("repeated schema init changed existing state", e)
			}
			out, e := exec.CommandContext(ctx, "node", "../../scripts/check-historical-sql.mjs", "--version", fmt.Sprint(version), "--empty-current", "--stdio").Output()
			if e != nil {
				t.Fatal("fresh source restore oracle failed")
			}
			var v historicalFrame
			if json.Unmarshal(out, &v) != nil {
				t.Fatal("invalid source restore frame")
			}
			restored, e := (backup.Repository{DB: db}).Restore(ctx, []byte(v.Body))
			if e != nil || restored.SourceVersion != version || restored.Rows != v.Rows || restored.Generation == initial {
				t.Fatal("new schema restore failed", e)
			}
			if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("fresh schema restored rows differ from source SQLite bootstrap")
			}
			exported := f.download()
			cmd := exec.CommandContext(ctx, "node", "../../scripts/check-v39-export.mjs")
			cmd.Stdin = bytes.NewReader(exported)
			if e = cmd.Run(); e != nil {
				t.Fatal("source rejected fresh schema re-export")
			}
			againRestore, e := (backup.Repository{DB: db}).Restore(ctx, exported)
			if e != nil || againRestore.SourceVersion != 39 || againRestore.Generation == restored.Generation || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("new schema v39 roundtrip changed rows", e)
			}
			state, gen := f.snapshot(), f.generation()
			again, e = dbschema.Initialize(ctx, db)
			if e != nil || again.Applied != 0 || again.Seeded || gen != f.generation() || !reflect.DeepEqual(state, f.snapshot()) {
				t.Fatal("init modified a complete populated database", e)
			}
		})
	}
	t.Run("populated partial refused", func(t *testing.T) {
		ctx, db := openBlankSchemaFixture(t)
		policy := dbschema.Trusted()
		for _, a := range policy.Actions {
			if a.Kind == "table" && a.Table == "agency_identity" {
				if _, e := db.ExecContext(ctx, a.SQL); e != nil {
					t.Fatal(e)
				}
			}
		}
		if _, e := db.ExecContext(ctx, "INSERT INTO agency_identity(agency_name) VALUES('CI retained partial row')"); e != nil {
			t.Fatal(e)
		}
		before, e := dbschema.Inspect(ctx, db)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = dbschema.Initialize(ctx, db); !errors.Is(e, dbschema.ErrPopulated) {
			t.Fatal("populated partial database accepted", e)
		}
		after, e := dbschema.Inspect(ctx, db)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("partial refusal changed schema", e)
		}
		var count int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agency_identity WHERE agency_name='CI retained partial row'").Scan(&count); e != nil || count != 1 {
			t.Fatal("partial init lost existing data")
		}
	})
	t.Run("empty partial with allocated IDs refused", func(t *testing.T) {
		ctx, db := openBlankSchemaFixture(t)
		for _, a := range dbschema.Trusted().Actions {
			if a.Kind == "table" && a.Table == "agency_identity" {
				if _, e := db.ExecContext(ctx, a.SQL); e != nil {
					t.Fatal(e)
				}
			}
		}
		if _, e := db.ExecContext(ctx, "INSERT INTO agency_identity(agency_name) VALUES('CI prior allocation')"); e != nil {
			t.Fatal(e)
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM agency_identity WHERE agency_name='CI prior allocation'"); e != nil {
			t.Fatal(e)
		}
		before, e := dbschema.Inspect(ctx, db)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = dbschema.Initialize(ctx, db); !errors.Is(e, dbschema.ErrPopulated) {
			t.Fatal("allocated empty partial database accepted", e)
		}
		after, e := dbschema.Inspect(ctx, db)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("allocated partial refusal changed schema")
		}
		var next uint64
		if e = db.QueryRowContext(ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='agency_identity'").Scan(&next); e != nil || next != 2 {
			t.Fatal("allocated partial refusal changed ID high-water")
		}

	})
	t.Run("foreign empty schema refused", func(t *testing.T) {
		ctx, db := openBlankSchemaFixture(t)
		if _, e := db.ExecContext(ctx, "CREATE TABLE ci_unrelated(id INT PRIMARY KEY)"); e != nil {
			t.Fatal(e)
		}
		before, e := dbschema.Inspect(ctx, db)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = dbschema.Initialize(ctx, db); e == nil {
			t.Fatal("foreign schema accepted")
		}
		after, e := dbschema.Inspect(ctx, db)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("foreign schema refusal executed DDL")
		}
	})

}
