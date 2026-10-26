//go:build mariadb && (vacancycreates || vacancyedits || vacancydeletes)

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"itmap/internal/roles"
	"itmap/internal/vacancies"
)

const crudCompany = "CI Synthetic CRUD Employer"
const crudMoveCompany = "CI Synthetic CRUD Move Target"
const crudURLPrefix = "https://example.invalid/go088/"
const crudChannelUsername = "ci_go088_channel"

type vacancyCreateFixture struct {
	t                              *testing.T
	ctx                            context.Context
	db                             *sql.DB
	repo                           vacancies.Repository
	root, leaf, otherLeaf, channel int64
}

func openVacancyCreateFixture(t *testing.T) *vacancyCreateFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("vacancy creation requires disposable GitHub CI")
		}
		t.Skip("vacancy creation requires disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &vacancyCreateFixture{t: t, ctx: ctx, db: db, repo: vacancies.Repository{DB: db}}
	if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name IN (?,?)`, crudCompany, crudMoveCompany) != 0 || f.count(`SELECT COUNT(*) FROM company_vacancies WHERE url LIKE ?`, crudURLPrefix+"%") != 0 || f.count(`SELECT COUNT(*) FROM vacancy_roles WHERE stable_key LIKE 'ci-go088-%'`) != 0 || f.count(`SELECT COUNT(*) FROM telegram_channels WHERE normalized_username=?`, crudChannelUsername) != 0 {
		t.Fatal("vacancy creation fixture requires unused synthetic namespace")
	}
	original, err := f.repo.DetailByID(ctx, "Eugene", 1)
	if err != nil {
		t.Fatal("original synthetic vacancy unavailable")
	}
	originalRoles, err := (roles.Repository{DB: db}).Read(ctx)
	if err != nil {
		t.Fatal("original synthetic roles unavailable")
	}
	var catalog, updated string
	f.scan(`SELECT data_json,updated_at FROM app_catalog WHERE id=1`, nil, &catalog, &updated)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM company_vacancies WHERE url LIKE ?`, []any{crudURLPrefix + "%"}},
			{`DELETE FROM company_crm WHERE company_name IN (?,?)`, []any{crudCompany, crudMoveCompany}},
			{`DELETE FROM company_crm WHERE company_name=CONCAT('__telegram_channel__:',?)`, []any{f.channel}},
			{`DELETE FROM telegram_channels WHERE id=? AND normalized_username=?`, []any{f.channel, crudChannelUsername}},
			{`DELETE FROM vacancy_roles WHERE id=? AND stable_key='ci-go088-other-leaf'`, []any{f.otherLeaf}},
			{`DELETE FROM vacancy_roles WHERE id=? AND stable_key='ci-go088-leaf'`, []any{f.leaf}},
			{`DELETE FROM vacancy_roles WHERE id=? AND stable_key='ci-go088-root'`, []any{f.root}},
			{`UPDATE app_catalog SET data_json=?,updated_at=? WHERE id=1`, []any{catalog, updated}},
		} {
			if _, err := db.ExecContext(cleanup, q.sql, q.args...); err != nil {
				t.Errorf("vacancy creation cleanup failed: %T", err)
			}
		}
		after, err := f.repo.DetailByID(cleanup, "Eugene", 1)
		if err != nil || !reflect.DeepEqual(original, after) {
			t.Error("vacancy creation changed seed vacancy/application/task projection")
		}
		afterRoles, err := (roles.Repository{DB: db}).Read(cleanup)
		if err != nil || !reflect.DeepEqual(originalRoles, afterRoles) {
			t.Error("vacancy creation changed original role directory")
		}
		var remaining int
		var generation string
		if err = db.QueryRowContext(cleanup, `SELECT (SELECT COUNT(*) FROM company_vacancies WHERE url LIKE ?)+(SELECT COUNT(*) FROM company_crm WHERE company_name=?)+(SELECT COUNT(*) FROM telegram_channels WHERE normalized_username=?)`, crudURLPrefix+"%", crudCompany, crudChannelUsername).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("vacancy creation fixture leaked owned rows")
		}
		if err = db.QueryRowContext(cleanup, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&generation); err != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("vacancy creation changed runtime generation")
		}
	})
	var data map[string]json.RawMessage
	if err = json.Unmarshal([]byte(catalog), &data); err != nil {
		t.Fatal("invalid synthetic catalog")
	}
	var companies []json.RawMessage
	if err = json.Unmarshal(data["companies"], &companies); err != nil {
		t.Fatal("invalid synthetic companies")
	}
	extra, _ := json.Marshal(map[string]any{"name": crudCompany, "sector": "CI test", "offices": []any{}, "jobs": []any{}, "vacancySources": map[string]any{}})
	companies = append(companies, extra)
	move, _ := json.Marshal(map[string]any{"name": crudMoveCompany, "sector": "CI test", "offices": []any{}, "jobs": []any{}, "vacancySources": map[string]any{}})
	companies = append(companies, move)
	data["companies"], _ = json.Marshal(companies)
	encoded, _ := json.Marshal(data)
	f.exec(`UPDATE app_catalog SET data_json=? WHERE id=1`, string(encoded))
	f.root = f.insert(`INSERT INTO vacancy_roles(stable_key,name) VALUES('ci-go088-root','CI GO088 Root')`)
	f.leaf = f.insert(`INSERT INTO vacancy_roles(stable_key,name,parent_id) VALUES('ci-go088-leaf','CI GO088 Leaf',?)`, f.root)
	f.otherLeaf = f.insert(`INSERT INTO vacancy_roles(stable_key,name,parent_id,sort_order) VALUES('ci-go088-other-leaf','CI GO088 Other Leaf',?,1)`, f.root)

	f.channel = f.insert(`INSERT INTO telegram_channels(name,url,normalized_username,username) VALUES('CI GO088 Channel','https://t.me/ci_go088_channel',?,'@ci_go088_channel')`, crudChannelUsername)
	return f
}
func (f *vacancyCreateFixture) scan(q string, args []any, dest ...any) {
	f.t.Helper()
	if err := f.db.QueryRowContext(f.ctx, q, args...).Scan(dest...); err != nil {
		f.t.Fatalf("vacancy creation fixture query failed: %T", err)
	}
}
func (f *vacancyCreateFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	f.scan(q, args, &n)
	return n
}
func (f *vacancyCreateFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("vacancy creation fixture statement failed: %T", err)
	}
}
func (f *vacancyCreateFixture) insert(q string, args ...any) int64 {
	f.t.Helper()
	res, err := f.db.ExecContext(f.ctx, q, args...)
	if err != nil {
		f.t.Fatalf("vacancy creation fixture insert failed: %T", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id < 1 {
		f.t.Fatal("vacancy creation fixture ID unavailable")
	}
	return id
}
func (f *vacancyCreateFixture) input(suffix string) vacancies.Input {
	relevance := int64(63)
	low, high := int64(100000), int64(200000)
	return vacancies.Input{CompanyName: crudCompany, EntityType: "company", URL: crudURLPrefix + suffix, Title: "CI synthetic manual vacancy", RoleIDs: []int64{f.leaf, f.leaf}, Level: "Senior", Relevance: &relevance, SalaryFrom: &low, SalaryTo: &high, PublishedAt: "2026-10-26", LoadedAt: "2026-10-26", DescriptionMD: "CI synthetic description <&> 😀", CoverLetterSubject: "CI shared subject", CoverLetterMD: "CI shared letter", TelegramChannelID: &f.channel, TelegramMessageURL: "https://telegram.me/s/ci_go088_channel/123?utm_source=ci", TelegramContact: "@ci_synthetic_contact"}
}
func (f *vacancyCreateFixture) id(url string) int64 {
	f.t.Helper()
	var id int64
	f.scan(`SELECT id FROM company_vacancies WHERE url=?`, []any{url}, &id)
	return id
}
func (f *vacancyCreateFixture) assertAbsent() {
	f.t.Helper()
	if f.count(`SELECT COUNT(*) FROM company_vacancies WHERE url LIKE ?`, crudURLPrefix+"%") != 0 || f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudCompany) != 0 || f.count(`SELECT COUNT(*) FROM vacancy_role_assignments WHERE role_id IN (?,?)`, f.root, f.leaf) != 0 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE channel_id=?`, f.channel) != 0 {
		f.t.Fatal("failed creation left partial parent/vacancy/role/provenance rows")
	}
}
func (f *vacancyCreateFixture) assertLockFree() {
	f.t.Helper()
	_, db := openDisposableMariaDB(f.t)
	conn, err := db.Conn(f.ctx)
	if err != nil {
		f.t.Fatal("cannot acquire independent named-lock probe")
	}
	defer conn.Close()
	var acquired int
	if err = conn.QueryRowContext(f.ctx, `SELECT GET_LOCK('itmap:manual-vacancy:create',0)`).Scan(&acquired); err != nil || acquired != 1 {
		f.t.Fatal("vacancy creator leaked its connection-scoped named lock")
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err = conn.ExecContext(cleanup, `DO RELEASE_LOCK('itmap:manual-vacancy:create')`); err != nil {
		f.t.Fatal("cannot release named-lock probe")
	}
}
func (f *vacancyCreateFixture) created(repo vacancies.Repository, input vacancies.Input) vacancies.CompanyVacancy {
	f.t.Helper()
	list, err := repo.CreateManual(f.ctx, "Demo", input)
	if err != nil {
		f.t.Fatalf("manual vacancy create failed: %T", err)
	}
	for _, v := range list {
		if v.URL == input.URL {
			return v
		}
	}
	f.t.Fatal("create response missed the inserted vacancy")
	return vacancies.CompanyVacancy{}
}
