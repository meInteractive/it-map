//go:build mariadb && contactwrites

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"itmap/internal/backup"
	"itmap/internal/contacts"
	"os"
	"os/exec"
	"testing"
	"time"
)

type contactFrame struct {
	Catalog         json.RawMessage
	Company, Agency string
	Steps           []struct {
		Name, Method, Owner string
		ID                  *int64
		Payload             json.RawMessage
		Status              int
		Result              json.RawMessage
		Error               string
	}
}
type contactFixture struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	repo   contacts.Repository
	source contactFrame
}

func openContactFixture(t *testing.T) *contactFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("contact writes require disposable GitHub CI")
		}
		t.Skip("contact writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &contactFixture{t: t, ctx: ctx, db: db, repo: contacts.Repository{DB: db, CatalogNames: backup.StoredCatalogNames}}
	tables := []string{"hiring_contacts", "company_crm", "app_catalog"}
	for _, table := range tables {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("contact fixture requires empty synthetic tables")
		}
	}
	out, e := exec.CommandContext(ctx, "node", "../../scripts/check-contact-storage.mjs", "--stdio").Output()
	if e != nil || json.Unmarshal(out, &f.source) != nil || len(f.source.Steps) != 21 {
		t.Fatal("required unchanged contact source unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			if _, e := db.ExecContext(ctx, "DELETE FROM "+table); e != nil {
				t.Error("cannot clean synthetic contact rows")
				return
			}
			var n int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
				t.Error("contact fixture leaked rows")
			}
		}
		var generation string
		if e := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("contact writes changed generation")
		}
	})
	f.exec("INSERT INTO app_catalog(id,data_json)VALUES(1,?)", string(f.source.Catalog))
	return f
}
func (f *contactFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.db.ExecContext(f.ctx, q, args...); e != nil {
		f.t.Fatalf("synthetic contact statement failed: %T", e)
	}
}
func (f *contactFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if e := f.db.QueryRowContext(f.ctx, q, args...).Scan(&n); e != nil {
		f.t.Fatalf("synthetic contact query failed: %T", e)
	}
	return n
}
func contactRaw(position int, name string) contacts.Input {
	data, _ := json.Marshal(map[string]any{"position": position, "fullName": name})
	var in contacts.Input
	_ = json.Unmarshal(data, &in)
	return in
}
func (f *contactFixture) create(company string, position int) contacts.Contact {
	f.t.Helper()
	out, e := f.repo.Create(f.ctx, company, contactRaw(position, "CI Contact"))
	if e != nil {
		f.t.Fatalf("synthetic contact create failed: %T", e)
	}
	for _, c := range out.Contacts {
		if c.Position == position {
			return c
		}
	}
	f.t.Fatal("created contact missing")
	return contacts.Contact{}
}
func (f *contactFixture) list(company string) []contacts.Contact {
	f.t.Helper()
	out, e := f.repo.List(f.ctx, company)
	if e != nil {
		f.t.Fatal("contact list failed")
	}
	return out
}
