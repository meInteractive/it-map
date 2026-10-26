//go:build mariadb && profilewrites && v39exports

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/backup"
	"itmap/internal/v39"
)

func TestMariaDBV39ExportDisposable(t *testing.T) {
	t.Run("all rows columns long text settings and source acceptance through SELECT-only credentials", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		reader := exportReadOnlyPool(f)
		f.save("Demo", "  CI 😀 <&> \u2028 \\u2028 "+strings.Repeat("long CI text\n", 1200), 0)
		for _, kind := range []string{"cover_letter", "relevance", "vacancy_extraction", "vacancy_role"} {
			f.savePrompt(kind, "  CI source text 😀  ", 0)
			f.saveModel(kind, freeProfileModel(0))
		}
		beforeProfiles, beforePrompts, beforeModels := f.read("Demo"), f.prompts(), f.models()
		migrationRow, e := f.db.ExecContext(f.ctx, `INSERT INTO __appgarden_migrations(name,applied_at) VALUES('CI v39 export marker','CI fixed time')`)
		if e != nil {
			t.Fatal("cannot seed synthetic migration metadata")
		}
		migrationID, e := migrationRow.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if _, e := f.db.ExecContext(context.Background(), `DELETE FROM __appgarden_migrations WHERE id=? AND name='CI v39 export marker'`, migrationID); e != nil {
				t.Error("cannot clean synthetic migration metadata")
			}
		})
		_, e = reader.ExecContext(f.ctx, `INSERT INTO system_prompts(kind) VALUES('CI_forbidden')`)
		var denied *mysql.MySQLError
		if !errors.As(e, &denied) || denied.Number != 1142 {
			t.Fatal("export reader unexpectedly has write privileges")
		}
		file, e := (backup.Repository{DB: reader}).Export(f.ctx)
		if e != nil {
			t.Fatalf("SQL export failed: %T", e)
		}
		p := exportPayload(t, file)
		if len(p.Storage.Tables) != 52 || string(p.Metadata.MissingTables) != "[]" {
			t.Fatal("export omitted existing application tables")
		}
		migrationFound := false
		for _, raw := range p.Metadata.Migrations {
			var m struct {
				Name string
				Rows []map[string]any
			}
			if json.Unmarshal(raw, &m) != nil {
				t.Fatal("invalid migration projection")
			}
			if m.Name == "__appgarden_migrations" {
				for _, r := range m.Rows {
					if r["id"] == float64(migrationID) && r["name"] == "CI v39 export marker" && r["applied_at"] == "CI fixed time" {
						migrationFound = true
					}
				}
			}
		}
		if !migrationFound {
			t.Fatal("migration metadata was omitted/normalized")
		}
		for _, definition := range v39.Trusted().Tables {
			table := exportTable(t, p, definition.Name)
			var n int
			if e = f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+definition.Name+"`").Scan(&n); e != nil || n != len(table.Rows) || len(table.Columns) != len(definition.Columns) {
				t.Fatal("export truncated rows/columns")
			}
			// Compare every scalar against a separate direct SQL read, without the export converter.
			names := make([]string, len(definition.Columns))
			for i, c := range definition.Columns {
				names[i] = "`" + c.Name + "`"
			}
			rows, e := f.db.QueryContext(f.ctx, "SELECT "+strings.Join(names, ",")+" FROM `"+definition.Name+"`")
			if e != nil {
				t.Fatal("independent export comparison query failed")
			}
			i := 0
			for rows.Next() {
				values := make([]any, len(names))
				ptr := make([]any, len(names))
				for j := range values {
					ptr[j] = &values[j]
				}
				if e = rows.Scan(ptr...); e != nil {
					t.Fatal("cannot compare export values")
				}
				for j, c := range definition.Columns {
					var actual any
					if e = json.Unmarshal(table.Rows[i][c.Name], &actual); e != nil {
						t.Fatal("invalid scalar in export")
					}
					want := values[j]
					switch v := want.(type) {
					case []byte:
						want = string(v)
					case int64:
						want = float64(v)
					}
					if !reflect.DeepEqual(actual, want) {
						t.Fatalf("export changed scalar in %s.%s", definition.Name, c.Name)
					}
				}
				i++
			}
			e = rows.Err()
			rows.Close()
			if e != nil || i != len(table.Rows) {
				t.Fatal("incomplete independent SQL comparison")
			}
		}
		for _, table := range p.Storage.Tables {
			for _, op := range v39.Trusted().OperationalTables {
				if table.Name == op.Name {
					t.Fatal("operational data exported")
				}
			}
		}
		cmd := exec.CommandContext(f.ctx, "node", filepath.Join("..", "..", "scripts", "check-v39-export.mjs"))
		cmd.Stdin = bytes.NewReader(file.Bytes)
		if out, e := cmd.CombinedOutput(); e != nil || !bytes.Contains(out, []byte("PASS: unchanged legacy validator")) {
			t.Fatal("unchanged legacy validator rejected synthetic SQL export")
		}
		if !reflect.DeepEqual(beforeProfiles, f.read("Demo")) || !reflect.DeepEqual(beforePrompts, f.prompts()) || !reflect.DeepEqual(beforeModels, f.models()) {
			t.Fatal("export normalized source data")
		}
	})
	t.Run("absent stored catalog gets unchanged source default without inserts", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		var original, updated string
		if e := f.db.QueryRowContext(f.ctx, `SELECT data_json,updated_at FROM app_catalog WHERE id=1`).Scan(&original, &updated); e != nil {
			t.Fatal("cannot snapshot synthetic catalog")
		}
		f.exec(`DELETE FROM app_catalog WHERE id=1`)
		t.Cleanup(func() {
			_, e := f.db.ExecContext(context.Background(), `INSERT INTO app_catalog(id,data_json,updated_at) VALUES(1,?,?)`, original, updated)
			if e != nil {
				t.Error("cannot restore synthetic catalog")
			}
		})
		file, e := (backup.Repository{DB: exportReadOnlyPool(f)}).Export(f.ctx)
		if e != nil {
			t.Fatalf("default catalog export failed: %T", e)
		}
		p := exportPayload(t, file)
		var actual, expected any
		_ = json.Unmarshal(p.Catalog, &actual)
		_ = json.Unmarshal(v39.DefaultCatalog(), &expected)
		if !reflect.DeepEqual(actual, expected) || len(exportTable(t, p, "app_catalog").Rows) != 0 || f.count(`SELECT COUNT(*) FROM app_catalog WHERE id=1`) != 0 {
			t.Fatal("default catalog changed or export inserted a row")
		}
	})
	t.Run("non-MVCC counter retains rollback allocation", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		result, e := tx.ExecContext(f.ctx, `INSERT INTO agency_identity(agency_name) VALUES('CI v39 rolled back ID')`)
		if e != nil {
			tx.Rollback()
			t.Fatal("cannot allocate disposable ID")
		}
		id, e := result.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.Rollback(); e != nil {
			t.Fatal(e)
		}
		file, e := (backup.Repository{DB: exportReadOnlyPool(f)}).Export(f.ctx)
		if e != nil {
			t.Fatalf("counter export failed: %T", e)
		}
		p := exportPayload(t, file)
		found := false
		for _, s := range p.Metadata.Sequences {
			if s.Name == "agency_identity" {
				found = s.Seq >= float64(id)
			}
		}
		if !found {
			t.Fatal("export lowered counter after rollback")
		}
	})
	t.Run("unknown table and column fail closed in synthetic schema only", func(t *testing.T) {
		for _, kind := range []string{"table", "column"} {
			t.Run(kind, func(t *testing.T) {
				f := openProfileSettingsFixture(t)
				var add, drop string
				if kind == "table" {
					add = "CREATE TABLE CI_v39_unknown(id BIGINT) ENGINE=InnoDB"
					drop = "DROP TABLE CI_v39_unknown"
				} else {
					add = "ALTER TABLE user_profiles ADD COLUMN CI_v39_extra TEXT"
					drop = "ALTER TABLE user_profiles DROP COLUMN CI_v39_extra"
				}
				f.exec(add)
				t.Cleanup(func() {
					if _, e := f.db.ExecContext(context.Background(), drop); e != nil {
						t.Error("cannot restore disposable schema")
					}
				})
				file, e := (backup.Repository{DB: exportReadOnlyPool(f)}).Export(f.ctx)
				if backup.ErrorStatus(e) != 400 || len(file.Bytes) != 0 {
					t.Fatal("unknown schema silently exported or partial output returned")
				}
			})
		}
	})
	t.Run("late query fault returns no file and fresh reader can retry", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		hook := &exportIntercept{prefix: "SELECT `user_name`,`resume_md`", fail: true}
		reader := interceptedExportPool(f, hook)
		file, e := (backup.Repository{DB: reader}).Export(f.ctx)
		if !errors.Is(e, errExportQuery) || len(file.Bytes) != 0 {
			t.Fatal("late export SQL failure returned partial data")
		}
		hook.mu.Lock()
		fired, ro := hook.fired, hook.readOnly
		hook.mu.Unlock()
		if !fired || !ro {
			t.Fatal("export did not use an actual read-only transaction")
		}
		reader.Close()
		file, e = (backup.Repository{DB: exportReadOnlyPool(f)}).Export(f.ctx)
		if e != nil {
			t.Fatal("fresh-pool export retry failed")
		}
		exportPayload(t, file)
	})
	t.Run("concurrent writes remain outside snapshot and counter never trails allocation", func(t *testing.T) { testV39ExportBarrier(t, false) })
	t.Run("concurrent schema addition invalidates export", func(t *testing.T) { testV39ExportBarrier(t, true) })
}

func testV39ExportBarrier(t *testing.T, ddl bool) {
	f := openProfileSettingsFixture(t)
	f.save("Demo", "CI snapshot before", 0)
	hook := &exportIntercept{prefix: "SELECT `id`,`data_json`", entered: make(chan struct{}), release: make(chan struct{})}
	reader := interceptedExportPool(f, hook)
	var file backup.Download
	var outcome error
	done := make(chan struct{})
	go func() { defer close(done); file, outcome = (backup.Repository{DB: reader}).Export(f.ctx) }()
	released := false
	t.Cleanup(func() {
		if !released {
			close(hook.release)
		}
		<-done
	})
	waitExportBarrier(t, hook)
	var allocated int64
	if ddl {
		f.exec(`CREATE TABLE CI_v39_concurrent(id BIGINT) ENGINE=InnoDB`)
		t.Cleanup(func() {
			if _, e := f.db.ExecContext(context.Background(), `DROP TABLE CI_v39_concurrent`); e != nil {
				t.Error("cannot clean concurrent synthetic table")
			}
		})
	} else {
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		result, e := tx.ExecContext(f.ctx, `INSERT INTO agency_identity(agency_name) VALUES('CI v39 concurrent ID')`)
		if e != nil {
			t.Fatal("concurrent ID allocation failed")
		}
		allocated, e = result.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(f.ctx, `UPDATE user_profiles SET resume_md='CI committed after snapshot',revision=revision+1 WHERE user_name='Demo'`); e != nil {
			t.Fatal("concurrent resume save failed")
		}
		if e = tx.Commit(); e != nil {
			t.Fatal("concurrent writer commit failed")
		}
		t.Cleanup(func() {
			if _, e := f.db.ExecContext(context.Background(), `DELETE FROM agency_identity WHERE id=? AND agency_name='CI v39 concurrent ID'`, allocated); e != nil {
				t.Error("cannot clean concurrent synthetic ID")
			}
		})
	}
	close(hook.release)
	released = true
	<-done
	if ddl {
		if backup.ErrorStatus(outcome) != 400 || len(file.Bytes) != 0 {
			t.Fatal("concurrent schema change was silently exported")
		}
		return
	}
	if outcome != nil {
		t.Fatalf("concurrent snapshot failed: %T", outcome)
	}
	p := exportPayload(t, file)
	profiles := exportTable(t, p, "user_profiles")
	found := false
	for _, r := range profiles.Rows {
		if string(r["user_name"]) == `"Demo"` {
			found = string(r["resume_md"]) == `"CI snapshot before"` && string(r["revision"]) == "1"
		}
	}
	if !found || f.read("Demo").ResumeMD != "CI committed after snapshot" {
		t.Fatal("read-only export mixed application snapshots")
	}
	for _, r := range exportTable(t, p, "agency_identity").Rows {
		if string(r["agency_name"]) == `"CI v39 concurrent ID"` {
			t.Fatal("snapshot contains a later insert")
		}
	}
	found = false
	for _, s := range p.Metadata.Sequences {
		if s.Name == "agency_identity" {
			found = s.Seq >= float64(allocated)
		}
	}
	if !found {
		t.Fatal("non-MVCC counter trails concurrent allocation")
	}
}
