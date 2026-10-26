//go:build mariadb && crmwrites

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/crm"
)

const crmFlagCompany = "CI Synthetic Employer"

var crmFreshCompany = "CI CRM " + strings.Repeat("я", 130)

type crmFlagFixture struct {
	t    *testing.T
	ctx  context.Context
	db   *sql.DB
	repo crm.Repository
}

func openCRMFlagFixture(t *testing.T) *crmFlagFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("CRM writes require disposable GitHub CI")
		}
		t.Skip("CRM writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &crmFlagFixture{t: t, ctx: ctx, db: db, repo: crm.Repository{DB: db}}
	var catalog, updated, generation, parent string
	f.scan(`SELECT data_json,updated_at FROM app_catalog WHERE id=1`, nil, &catalog, &updated)
	f.scan(`SELECT generation FROM app_runtime_generation WHERE id=1`, nil, &generation)
	f.scan(`SELECT JSON_ARRAY(status,comment_md,updated_at,favorite) FROM company_crm WHERE company_name=?`, []any{crmFlagCompany}, &parent)
	if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crmFreshCompany) != 0 || f.count(`SELECT COUNT(*) FROM company_user_state WHERE user_name IN ('Demo','Nastya') AND company_name IN (?,?)`, crmFlagCompany, crmFreshCompany) != 0 || f.count(`SELECT COUNT(*) FROM company_status_history WHERE user_name IN ('Demo','Nastya') AND company_name IN (?,?)`, crmFlagCompany, crmFreshCompany) != 0 {
		t.Fatal("CRM fixture requires unused synthetic owners")
	}
	before := f.state("Eugene", crmFlagCompany)
	// Register cleanup before the first write; use an independent bounded context.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM company_status_history WHERE user_name IN ('Demo','Nastya') AND company_name IN (?,?)`, `DELETE FROM company_user_state WHERE user_name IN ('Demo','Nastya') AND company_name IN (?,?)`} {
			if _, err := db.ExecContext(cleanup, q, crmFlagCompany, crmFreshCompany); err != nil {
				t.Error("cannot clean synthetic CRM state")
			}
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM company_crm WHERE company_name=?`, crmFreshCompany); err != nil {
			t.Error("cannot clean synthetic CRM bootstrap")
		}
		if _, err := db.ExecContext(cleanup, `UPDATE app_catalog SET data_json=?,updated_at=? WHERE id=1`, catalog, updated); err != nil {
			t.Error("cannot restore synthetic catalog")
		}
		var afterParent, afterGeneration string
		if err := db.QueryRowContext(cleanup, `SELECT JSON_ARRAY(status,comment_md,updated_at,favorite) FROM company_crm WHERE company_name=?`, crmFlagCompany).Scan(&afterParent); err != nil || afterParent != parent {
			t.Error("global CRM parent changed")
		}
		if err := db.QueryRowContext(cleanup, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&afterGeneration); err != nil || afterGeneration != generation {
			t.Error("CRM changed runtime generation")
		}
		after, err := f.repo.Company(cleanup, "Eugene", crmFlagCompany)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Error("CRM changed another owner's state/history")
		}
		var remaining int
		if err := db.QueryRowContext(cleanup, `SELECT COUNT(*) FROM company_user_state WHERE user_name IN ('Demo','Nastya') AND company_name IN (?,?)`, crmFlagCompany, crmFreshCompany).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("CRM cleanup left private state")
		}
	})
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(catalog), &data); err != nil {
		t.Fatal("invalid synthetic catalog")
	}
	var companies []json.RawMessage
	if err := json.Unmarshal(data["companies"], &companies); err != nil {
		t.Fatal("invalid synthetic company list")
	}
	extra, _ := json.Marshal(map[string]string{"name": crmFreshCompany})
	companies = append(companies, extra)
	data["companies"], _ = json.Marshal(companies)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal("cannot encode synthetic catalog")
	}
	f.exec(`UPDATE app_catalog SET data_json=? WHERE id=1`, string(encoded))
	return f
}
func (f *crmFlagFixture) scan(q string, args []any, dest ...any) {
	f.t.Helper()
	if err := f.db.QueryRowContext(f.ctx, q, args...).Scan(dest...); err != nil {
		f.t.Fatalf("CRM fixture query failed: %T", err)
	}
}
func (f *crmFlagFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	f.scan(q, args, &n)
	return n
}
func (f *crmFlagFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("CRM fixture statement failed: %T", err)
	}
}
func (f *crmFlagFixture) state(user, company string) crm.CompanyState {
	f.t.Helper()
	v, e := f.repo.Company(f.ctx, user, company)
	if e != nil {
		f.t.Fatalf("CRM read failed: %T", e)
	}
	return v
}
func (f *crmFlagFixture) set(user, company, flag string, value bool) crm.CompanyState {
	f.t.Helper()
	v, e := f.repo.SetCompanyFlag(f.ctx, user, company, flag, value)
	if e != nil {
		f.t.Fatalf("CRM flag write failed: %T", e)
	}
	return v
}
func (f *crmFlagFixture) seed(user string) {
	f.t.Helper()
	f.exec(`INSERT INTO company_user_state(user_name,company_name,status,comment_md,updated_at) VALUES(?,?, 'CI retained status','CI retained comment','2026-10-26 00:00:00')`, user, crmFlagCompany)
	f.exec(`INSERT INTO company_status_history(user_name,company_name,from_status,to_status,changed_at) VALUES(?,?,'','CI retained status','2026-10-26 00:00:00')`, user, crmFlagCompany)
}
