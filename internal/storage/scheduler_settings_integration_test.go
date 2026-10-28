//go:build mariadb && schedulerwrites && schedulersettings

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/parser"
)

type settingsSQLFixture struct {
	t          *testing.T
	ctx        context.Context
	db, reader *sql.DB
	generation string
}
type settingsSQLCase struct {
	Name                                   string
	Raw                                    *string
	InitialRevision, Favorite              int64
	Legacy                                 []string
	Input                                  json.RawMessage
	OwnerName, Status, Message, Body, Read string
	Unrelated, Row                         map[string]any
}

func requireSettingsCI(t *testing.T) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("settings writes require disposable GitHub CI")
		}
		t.Skip("settings writes require disposable GitHub CI")
	}
}
func openSettingsSQL(t *testing.T) *settingsSQLFixture {
	t.Helper()
	_, db := schedulerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f := &settingsSQLFixture{t: t, ctx: ctx, db: db, generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}
	for _, table := range []string{"firecrawl_settings", "company_identity", "vacancy_source_parsing"} {
		var n int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("settings namespace must start empty")
		}
	}
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_DSN"))
	if e != nil || cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:3306" || cfg.DBName != "itmap_ci" || cfg.User != "itmap_readonly_ci" {
		t.Fatal("refusing non-disposable settings read DSN")
	}
	f.reader, e = sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal("cannot open guarded settings reader")
	}
	t.Cleanup(func() { f.reader.Close() })
	var generation string
	if e = f.reader.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || requireDisposableGeneration(f.generation, generation) != nil {
		t.Fatal("settings reader nonce mismatch")
	}
	_, e = f.reader.ExecContext(ctx, "UPDATE firecrawl_settings SET id=id WHERE id=-1")
	if me, ok := e.(*mysql.MySQLError); !ok || me.Number != 1142 {
		t.Fatal("settings reader must be SELECT-only")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range []string{"vacancy_source_parsing", "firecrawl_settings", "company_identity"} {
			if _, e := db.ExecContext(cleanup, "DELETE FROM "+table); e != nil {
				t.Error("settings fixture cleanup failed")
			}
		}
		if _, e := db.ExecContext(cleanup, "UPDATE app_runtime_generation SET generation=? WHERE id=1", f.generation); e != nil {
			t.Error("settings fixture nonce cleanup failed")
		}
	})
	return f
}
func (f *settingsSQLFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.db.ExecContext(f.ctx, q, args...); e != nil {
		f.t.Fatal("synthetic settings fixture SQL failed")
	}
}
func (f *settingsSQLFixture) seed(v settingsSQLCase) {
	f.t.Helper()
	for _, table := range []string{"vacancy_source_parsing", "firecrawl_settings", "company_identity"} {
		f.exec("DELETE FROM " + table)
	}
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(1,'CI settings owner')")
	for _, id := range v.Legacy {
		f.exec("INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,auto_enabled) VALUES(1,?,?,1)", id, "https://example.invalid/"+id)
	}
	if v.Raw != nil {
		f.exec("INSERT INTO firecrawl_settings(id,scheduler_json,favorites_only,revision) VALUES(1,?,?,?)", *v.Raw, v.Favorite, v.InitialRevision)
		// The oracle uses only these reviewed opaque, synthetic provider fields.
		for key, value := range v.Unrelated {
			switch key {
			case "key_encrypted", "key_suffix", "max_pages", "timeout_ms", "last_error":
			default:
				f.t.Fatal("unreviewed settings fixture field")
			}
			f.exec("UPDATE firecrawl_settings SET "+key+"=? WHERE id=1", value)
		}
	}
}
func (f *settingsSQLFixture) row() map[string]any {
	f.t.Helper()
	rows, e := f.db.QueryContext(f.ctx, "SELECT * FROM firecrawl_settings WHERE id=1")
	if e != nil {
		f.t.Fatal("cannot inspect synthetic settings row")
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			f.t.Fatal(rows.Err())
		}
		return nil
	}
	columns, e := rows.Columns()
	if e != nil {
		f.t.Fatal(e)
	}
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if e = rows.Scan(dest...); e != nil {
		f.t.Fatal(e)
	}
	row := map[string]any{}
	for i, key := range columns {
		if b, ok := values[i].([]byte); ok {
			row[key] = string(b)
		} else {
			row[key] = values[i]
		}
	}
	return row
}
func settingsCases(t *testing.T) []settingsSQLCase {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-scheduler-settings-sql.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	cases := []settingsSQLCase{}
	for {
		var v settingsSQLCase
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		cases = append(cases, v)
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required original settings SQL oracle failed", e)
	}
	if len(cases) != 42 {
		t.Fatal("missing source settings transitions", len(cases))
	}
	return cases
}
func settingsRowDifferences(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	out := []string{}
	for k := range keys {
		x, _ := json.Marshal(a[k])
		y, _ := json.Marshal(b[k])
		if string(x) != string(y) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
func TestMariaDBSchedulerSettingsSourceDisposable(t *testing.T) {
	f := openSettingsSQL(t)
	repo := parser.SettingsRepository{DB: f.db}
	for _, v := range settingsCases(t) {
		t.Run(v.Name, func(t *testing.T) {
			f.seed(v)
			body, e := repo.Save(f.ctx, v.Input, v.OwnerName)
			status, message := "ok", ""
			var input *parser.SettingsError
			switch {
			case errors.As(e, &input):
				status = "input"
				message = e.Error()
			case errors.Is(e, parser.ErrSettingsChanged):
				status = "changed"
			case e != nil:
				status = "storage"
			}
			if status != v.Status || message != v.Message || e == nil && string(body) != v.Body {
				t.Fatalf("source settings outcome differs %s/%s", status, v.Status)
			}
			if diff := settingsRowDifferences(f.row(), v.Row); len(diff) != 0 {
				t.Fatalf("source full-row columns differ: %v", diff)
			}
			read, e := (parser.SettingsRepository{DB: f.reader}).Read(f.ctx)
			if e != nil || string(read) != v.Read {
				t.Fatal("source SELECT-only public settings read differs")
			}
		})
	}
	t.Log("PASS: 42 unchanged SQLite full-row transitions match MariaDB, including JSON1 CAS/unknown private fields/shared favorites")
}

func TestMariaDBSchedulerSettingsOwnershipDisposable(t *testing.T) {
	requireSettingsCI(t)
	cases := settingsCases(t)
	t.Run("missing read does not initialize; invalid initial save rolls back", func(t *testing.T) {
		f := openSettingsSQL(t)
		repo := parser.SettingsRepository{DB: f.db}
		if _, e := (parser.SettingsRepository{DB: f.reader}).Read(f.ctx); e != nil || f.row() != nil {
			t.Fatal("missing read wrote or failed")
		}
		if _, e := repo.Save(f.ctx, json.RawMessage(`{"enabled":false,"revision":7,"dayBudgetUsd":0,"monthBudgetUsd":0}`), "Eugene"); !errors.Is(e, parser.ErrSettingsChanged) || f.row() != nil {
			t.Fatal("failed first CAS left initialized data")
		}
	})
	t.Run("two writers keep original CAS", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(cases[0])
		repo := parser.SettingsRepository{DB: f.db}
		start := make(chan struct{})
		done := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { <-start; _, e := repo.Save(f.ctx, cases[0].Input, "Eugene"); done <- e }()
		}
		close(start)
		ok, conflict := 0, 0
		for i := 0; i < 2; i++ {
			e := <-done
			if e == nil {
				ok++
			} else if errors.Is(e, parser.ErrSettingsChanged) {
				conflict++
			} else {
				t.Fatal("concurrent settings save failed outside CAS")
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatal("two writers did not serialize to one CAS winner")
		}
	})
	t.Run("shared Firecrawl change survives scheduler save", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(cases[0])
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(f.ctx, "UPDATE firecrawl_settings SET max_pages=6,revision=revision+1 WHERE id=1 AND revision=3"); e != nil {
			t.Fatal("synthetic independent Firecrawl writer failed")
		}
		done := make(chan error, 1)
		go func() { _, e := (parser.SettingsRepository{DB: f.db}).Save(f.ctx, cases[0].Input, "Eugene"); done <- e }()
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		if e = <-done; e != nil {
			t.Fatal("unrelated shared Firecrawl change conflicted with scheduler revision")
		}
		row := f.row()
		if row["revision"] != int64(4) || row["max_pages"] != int64(6) {
			t.Fatal("scheduler overwrote another writer's revision/fields")
		}
	})
	t.Run("shared favorites invalidates scheduler CAS", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(cases[0])
		f.exec(`UPDATE firecrawl_settings SET favorites_only=0,revision=revision+1,scheduler_json=JSON_SET(scheduler_json,'$.revision',8) WHERE id=1`)
		before := f.row()
		_, e := (parser.SettingsRepository{DB: f.db}).Save(f.ctx, cases[0].Input, "Eugene")
		if !errors.Is(e, parser.ErrSettingsChanged) || len(settingsRowDifferences(before, f.row())) != 0 {
			t.Fatal("shared favorites conflict lost data")
		}
	})
	t.Run("native shared revision overflow rolls back all fields", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(cases[0])
		f.exec("UPDATE firecrawl_settings SET revision=9223372036854775807 WHERE id=1")
		before := f.row()
		_, e := (parser.SettingsRepository{DB: f.db}).Save(f.ctx, json.RawMessage(`{"enabled":false,"revision":7,"dayBudgetUsd":0,"monthBudgetUsd":0,"favoritesOnly":false}`), "Eugene")
		if !errors.Is(e, parser.ErrSettingsStorage) || len(settingsRowDifferences(before, f.row())) != 0 {
			t.Fatal("native overflow partially persisted settings")
		}
	})
	t.Run("cancellation leaves row unchanged", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(cases[0])
		before := f.row()
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		if _, e := (parser.SettingsRepository{DB: f.db}).Save(ctx, cases[0].Input, "Eugene"); !errors.Is(e, context.Canceled) || len(settingsRowDifferences(before, f.row())) != 0 {
			t.Fatal("cancelled settings operation mutated data")
		}
	})
}
