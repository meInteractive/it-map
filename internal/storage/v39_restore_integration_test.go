//go:build mariadb && v39restore

package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"testing"
	"time"

	"itmap/internal/backup"
)

type restoreFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	body     []byte
	expected []backup.Table
	rows     int
	warnings []string
}

func openRestoreFixture(t *testing.T) *restoreFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("restore requires disposable GitHub CI")
		}
		t.Skip("restore requires disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &restoreFixture{t: t, ctx: ctx, db: db}
	original := f.download()
	var generation, updated string
	if e := db.QueryRowContext(ctx, "SELECT generation,updated_at FROM app_runtime_generation WHERE id=1").Scan(&generation, &updated); e != nil {
		t.Fatal("cannot inspect generation")
	}
	var vector struct {
		Body     string
		Expected []backup.Table
		Rows     int
		Warnings []string
	}
	cmd := exec.CommandContext(ctx, "node", "../../scripts/check-v39-restore-sql.mjs", "--stdio")
	out, e := cmd.Output()
	if e != nil || json.Unmarshal(out, &vector) != nil {
		t.Fatal("source synthetic restore oracle failed")
	}
	f.body = []byte(vector.Body)
	f.expected = vector.Expected
	f.rows = vector.Rows
	f.warnings = vector.Warnings
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Fixture-only cleanup restores the run nonce after exercising real rotation.
		for _, q := range []string{"DELETE FROM vacancy_candidate_classification_leases WHERE owner_token='CI restore owner'", "DELETE FROM scheduler_attempts WHERE id='CI restore spend'", "DELETE FROM application_creation_receipts WHERE user_name='CI restore owner'"} {
			if _, e := db.ExecContext(cleanup, q); e != nil {
				t.Error("cannot clean owned operational fixture")
			}
		}
		if _, e := (backup.Repository{DB: db}).RestoreV39(cleanup, original); e != nil {
			t.Errorf("cannot restore synthetic fixture baseline: %T", e)
			return
		}
		if _, e := db.ExecContext(cleanup, "UPDATE app_runtime_generation SET generation=?,updated_at=? WHERE id=1", generation, updated); e != nil {
			t.Error("cannot reset test-only generation")
		}
	})
	return f
}
func (f *restoreFixture) download() []byte {
	f.t.Helper()
	file, e := (backup.Repository{DB: f.db}).Export(f.ctx)
	if e != nil {
		f.t.Fatalf("synthetic export failed: %T", e)
	}
	return file.Bytes
}
func (f *restoreFixture) generation() string {
	f.t.Helper()
	var s string
	if e := f.db.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&s); e != nil {
		f.t.Fatal("generation read failed")
	}
	return s
}
func (f *restoreFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.db.ExecContext(f.ctx, q, args...); e != nil {
		f.t.Fatalf("synthetic SQL failed: %T", e)
	}
}
func (f *restoreFixture) count(q string) int {
	f.t.Helper()
	var n int
	if e := f.db.QueryRowContext(f.ctx, q).Scan(&n); e != nil {
		f.t.Fatal("synthetic count failed")
	}
	return n
}
func restoreRecords(t *testing.T, tables []backup.Table) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range tables {
		rows := []string{}
		for _, row := range table.Rows {
			raw, e := json.Marshal(row)
			if e != nil {
				t.Fatal(e)
			}
			rows = append(rows, string(raw))
		}
		sort.Strings(rows)
		out[table.Name] = rows
	}
	return out
}
func (f *restoreFixture) snapshot() map[string][]string {
	p, e := backup.ValidateV39(f.download())
	if e != nil {
		f.t.Fatal("bad synthetic snapshot")
	}
	return restoreRecords(f.t, p.Storage.Tables)
}
func (f *restoreFixture) restore(body []byte) backup.RestoreResult {
	f.t.Helper()
	r, e := (backup.Repository{DB: f.db}).RestoreV39(f.ctx, body)
	if e != nil {
		f.t.Fatalf("restore failed: %T (%v)", e, e)
	}
	return r
}
func (f *restoreFixture) resign(change func(*backup.Payload)) []byte {
	f.t.Helper()
	p, e := backup.ValidateV39(f.body)
	if e != nil {
		f.t.Fatal(e)
	}
	change(p)
	raw, e := json.Marshal(p)
	if e != nil {
		f.t.Fatal(e)
	}
	body, e := backup.EncodeV39(raw)
	if e != nil {
		f.t.Fatalf("cannot sign mutated fixture: %T", e)
	}
	return body
}
func restoreTable(p *backup.Payload, name string) *backup.Table {
	for i := range p.Storage.Tables {
		if p.Storage.Tables[i].Name == name {
			return &p.Storage.Tables[i]
		}
	}
	panic("missing trusted table")
}
func TestMariaDBV39RestoreDisposable(t *testing.T) {
	t.Run("source restore roundtrip preserves values receipts spend and counter highwater", func(t *testing.T) {
		f := openRestoreFixture(t)
		old := f.generation()
		f.exec("INSERT INTO user_profiles(user_name,resume_md) VALUES('CI obsolete owner','CI old data')")
		f.exec("INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash,created_application_id) VALUES('CI restore owner','11111111-1111-4111-8111-111111111111',?,?,1)", old, string(bytes.Repeat([]byte{'a'}, 64)))
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd,result_json) VALUES('CI restore spend','CI job','CI run','scan_source',1,'done',1.25,0.75,'{\"CI\":true}')")
		f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(900001,'CI old candidate','CI old title',?,'manual','CI','CI','CI','CI')", string(bytes.Repeat([]byte{'b'}, 64)))
		f.exec("INSERT INTO vacancy_candidate_classification_leases(candidate_id,generation,owner_token,expires_at) VALUES(900001,?,'CI restore owner',100)", old)
		result := f.restore(f.body)
		if result.Generation == old || f.generation() != result.Generation || result.Rows != f.rows || !reflect.DeepEqual(result.Warnings, f.warnings) {
			t.Fatal("restore response/generation differs")
		}
		if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
			t.Fatal("Go SQL rows differ from unchanged legacy restore")
		}
		if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='CI restore owner' AND created_application_id=1") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI restore spend' AND reserved_usd=1.25 AND cost_usd=0.75 AND result_json='{\"CI\":true}'") != 1 || f.count("SELECT COUNT(*) FROM vacancy_candidate_classification_leases WHERE owner_token='CI restore owner'") != 0 {
			t.Fatal("operational preservation or source cascade differs")
		}
		exported := f.download()
		cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-v39-export.mjs")
		cmd.Stdin = bytes.NewReader(exported)
		if e := cmd.Run(); e != nil {
			t.Fatal("legacy rejected SQL re-export")
		}
		again := f.restore(exported)
		if again.Generation == result.Generation || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
			t.Fatal("second roundtrip changed values or retained generation")
		}
		r, e := f.db.ExecContext(f.ctx, "INSERT INTO company_vacancies(company_name,url,title) VALUES('Synthetic company','https://example.invalid/after-restore','CI after')")
		if e != nil {
			t.Fatal("post-restore allocation failed")
		}
		id, _ := r.LastInsertId()
		if id < 1000000 {
			t.Fatal("restore reused an ID below source highwater")
		}
	})
	for _, stage := range []string{"DELETE FROM `company_vacancies`", "INSERT INTO `user_profiles`", "UPDATE app_runtime_generation SET"} {
		t.Run("late fault rollback "+stage, func(t *testing.T) {
			f := openRestoreFixture(t)
			f.restore(f.body)
			before, gen := f.snapshot(), f.generation()
			hook := &restoreFault{prefix: stage}
			pool := restoreFaultPool(f, hook)
			if _, e := (backup.Repository{DB: pool}).RestoreV39(f.ctx, f.body); e == nil || !hook.fired {
				t.Fatal("late SQL fault did not fail")
			}
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("late fault committed partial data or generation")
			}
			f.restore(f.body)
			if !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("fresh retry differs")
			}
		})
	}
	t.Run("silent trigger mutation rejected with complete rollback", func(t *testing.T) {
		f := openRestoreFixture(t)
		f.restore(f.body)
		before, gen := f.snapshot(), f.generation()
		f.exec("CREATE TRIGGER ci_restore_mutation BEFORE INSERT ON user_profiles FOR EACH ROW SET NEW.resume_md='CI mutation'")
		defer f.exec("DROP TRIGGER ci_restore_mutation")
		if _, e := (backup.Repository{DB: f.db}).RestoreV39(f.ctx, f.body); e == nil {
			t.Fatal("silent mutation was accepted")
		}
		if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
			t.Fatal("value verification did not roll back")
		}
	})
	t.Run("SQL scalar losses rejected before connection and DDL", func(t *testing.T) {
		f := openRestoreFixture(t)
		for _, raw := range []string{`"\ud800"`, `7`, `null`} {
			body := f.resign(func(p *backup.Payload) { restoreTable(p, "user_profiles").Rows[0]["resume_md"] = json.RawMessage(raw) })
			// Nil repository proves rejection precedes DB acquisition/counter DDL.
			if _, e := (backup.Repository{}).RestoreV39(f.ctx, body); backup.ErrorStatus(e) != 400 {
				t.Fatal("lossy scalar reached storage acquisition")
			}
		}
		for _, raw := range []string{`9007199254740992`, `1.25`, `"7"`} {
			body := f.resign(func(p *backup.Payload) { restoreTable(p, "user_profiles").Rows[0]["revision"] = json.RawMessage(raw) })
			if _, e := (backup.Repository{}).RestoreV39(f.ctx, body); backup.ErrorStatus(e) != 400 {
				t.Fatal("lossy INTEGER reached storage acquisition")
			}
		}
	})
	for _, kind := range []string{"duplicate primary key", "CHECK constraint", "composite FK"} {
		t.Run(kind+" rollback", func(t *testing.T) {
			f := openRestoreFixture(t)
			f.restore(f.body)
			before, gen := f.snapshot(), f.generation()
			body := f.resign(func(p *backup.Payload) {
				switch kind {
				case "duplicate primary key":
					tab := restoreTable(p, "user_profiles")
					tab.Rows = append(tab.Rows, tab.Rows[0])
				case "composite FK":
					tab := restoreTable(p, "vacancy_parser_roles")
					tab.Rows = append(tab.Rows, map[string]json.RawMessage{"vacancy_id": json.RawMessage("1"), "role_id": json.RawMessage("8"), "candidate_id": json.RawMessage("11")})
				case "CHECK constraint":
					restoreTable(p, "company_vacancy_user_state").Rows[0]["relevance"] = json.RawMessage("101")
				}
			})
			if _, e := (backup.Repository{DB: f.db}).RestoreV39(f.ctx, body); e == nil {
				t.Fatal("invalid SQL rows accepted")
			}
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("SQL rejection did not roll back")
			}
		})
	}
}
