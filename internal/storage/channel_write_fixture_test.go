//go:build mariadb && channelwrites

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"itmap/internal/backup"
	"itmap/internal/channels"
	"os"
	"os/exec"
	"testing"
	"time"
)

type channelSourceFrame struct {
	Catalog json.RawMessage
	Steps   []struct {
		Name, Method string
		ID           *int64
		Payload      json.RawMessage
		Status       int
		Result       json.RawMessage
		Error        string
	}
	OrderNames []string
	Ordered    []channels.Channel
}
type channelFixture struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	repo   channels.Repository
	source channelSourceFrame
}

var channelTables = []string{"vacancy_telegram_sources", "company_vacancies", "telegram_channels", "company_crm", "app_catalog"}

func openChannelFixture(t *testing.T) *channelFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("channel writes require disposable GitHub CI")
		}
		t.Skip("channel writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &channelFixture{t: t, ctx: ctx, db: db, repo: channels.Repository{DB: db, CatalogSources: backup.StoredCatalogSourceURLs}}
	for _, table := range channelTables {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("channel fixture requires empty synthetic tables")
		}
	}
	out, e := exec.CommandContext(ctx, "node", "../../scripts/check-channel-storage.mjs", "--stdio").Output()
	if e != nil || json.Unmarshal(out, &f.source) != nil || len(f.source.Steps) != 17 {
		t.Fatal("required unchanged channel source unavailable")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range channelTables {
			if _, e := db.ExecContext(cleanup, "DELETE FROM "+table); e != nil {
				t.Error("cannot clean synthetic channel rows")
				return
			}
			var n int
			if e := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
				t.Error("channel fixture leaked rows")
			}
		}
		var generation string
		if e := db.QueryRowContext(cleanup, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("channel writes changed runtime generation")
		}
	})
	f.exec("INSERT INTO app_catalog(id,data_json)VALUES(1,?)", string(f.source.Catalog))
	return f
}
func (f *channelFixture) exec(q string, args ...any) int64 {
	f.t.Helper()
	r, e := f.db.ExecContext(f.ctx, q, args...)
	if e != nil {
		f.t.Fatalf("synthetic channel statement failed: %T", e)
	}
	id, _ := r.LastInsertId()
	return id
}
func (f *channelFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if e := f.db.QueryRowContext(f.ctx, q, args...).Scan(&n); e != nil {
		f.t.Fatalf("synthetic channel query failed: %T", e)
	}
	return n
}
func channelInput(name, user string) channels.Input {
	return channels.Input{Name: &name, Username: &user}
}
func (f *channelFixture) create(name, user string) int64 {
	f.t.Helper()
	v, e := f.repo.Create(f.ctx, channelInput(name, user))
	if e != nil || !v.OK {
		f.t.Fatalf("synthetic channel create failed: %T", e)
	}
	return v.ChannelID
}
func (f *channelFixture) vacancy(kind string) int64 {
	f.t.Helper()
	f.exec("INSERT IGNORE INTO company_crm(company_name)VALUES('CI Synthetic Channel Owner')")
	return f.exec("INSERT INTO company_vacancies(company_name,entity_type,url,title)VALUES('CI Synthetic Channel Owner',?,?,?)", kind, fmt.Sprintf("https://example.invalid/ci-%d", f.count("SELECT COUNT(*) FROM company_vacancies")), "CI channel vacancy")
}
func (f *channelFixture) link(v, c int64) {
	f.exec("INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url)VALUES(?,?,?)", v, c, fmt.Sprintf("https://t.me/ci_source_%d/%d", c, v))
}
func (f *channelFixture) list() []channels.Channel {
	f.t.Helper()
	v, e := f.repo.List(f.ctx)
	if e != nil {
		f.t.Fatal("cannot read channels")
	}
	return v
}
func (f *channelFixture) assertCatalog() {
	f.t.Helper()
	var data string
	if e := f.db.QueryRowContext(f.ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); e != nil || data != string(f.source.Catalog) {
		f.t.Fatal("channel mutation changed source catalog")
	}
}
