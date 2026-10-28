//go:build mariadb && catalogreads

package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/auth"
	"itmap/internal/catalog"
	"itmap/internal/httpapi"
)

var catalogTables = []string{"app_catalog", "company_identity", "agency_identity", "company_crm", "company_user_state", "company_vacancies", "company_applications", "company_vacancy_state"}

type catalogSQLFrame struct {
	Name     string
	Rows     map[string][]map[string]any
	Requests []struct {
		User, URL, Method, Body string
		Status                  int
	}
}

type catalogSQLFixture struct {
	t              *testing.T
	ctx            context.Context
	writer, reader *sql.DB
	baseline       schemaBaseline
}

func openCatalogSQL(t *testing.T) *catalogSQLFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("catalog reads require disposable GitHub CI")
		}
		t.Skip("catalog reads require disposable GitHub CI")
	}
	_, writer := openDisposableMariaDB(t)
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_DSN"))
	if e != nil || cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:3306" || cfg.DBName != "itmap_ci" || cfg.User != "itmap_readonly_ci" {
		t.Fatal("refusing non-disposable catalog read DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	reader, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal("cannot initialize catalog reader")
	}
	t.Cleanup(func() { reader.Close() })
	var gen string
	if e = reader.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&gen); e != nil || requireDisposableGeneration(os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION"), gen) != nil {
		t.Fatal("catalog reader generation mismatch")
	}
	// Even a zero-row UPDATE must be denied: a read-only Tx flag alone is not proof.
	_, e = reader.ExecContext(ctx, "UPDATE app_catalog SET id=id WHERE id=-1")
	if me, ok := e.(*mysql.MySQLError); !ok || me.Number != 1142 {
		t.Fatal("catalog reader must have SELECT-only privileges")
	}
	f := &catalogSQLFixture{t: t, ctx: ctx, writer: writer, reader: reader}
	if e = json.Unmarshal(baselineJSON, &f.baseline); e != nil {
		t.Fatal(e)
	}
	for _, table := range catalogTables {
		var n int
		if e = writer.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("catalog namespace must start empty")
		}
	}
	t.Cleanup(f.clear)
	return f
}

func (f *catalogSQLFixture) clear() {
	f.t.Helper()
	for i := len(catalogTables) - 1; i >= 0; i-- {
		if _, e := f.writer.ExecContext(f.ctx, "DELETE FROM "+catalogTables[i]); e != nil {
			f.t.Fatal("catalog synthetic cleanup failed")
		}
	}
}
func (f *catalogSQLFixture) seed(v catalogSQLFrame) {
	f.t.Helper()
	f.clear()
	for _, table := range catalogTables {
		for _, row := range v.Rows[table] {
			keys := []string{}
			for k := range row {
				if _, ok := f.baseline.Tables[table][k]; !ok {
					f.t.Fatal("unreviewed catalog fixture column")
				}
				keys = append(keys, k)
			}
			sort.Strings(keys)
			values := []any{}
			quoted := []string{}
			for _, k := range keys {
				values = append(values, row[k])
				quoted = append(quoted, "`"+k+"`")
			}
			q := "INSERT INTO " + table + "(" + strings.Join(quoted, ",") + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + ")"
			if _, e := f.writer.ExecContext(f.ctx, q, values...); e != nil {
				f.t.Fatalf("synthetic catalog seed %s: %v", table, e)
			}
		}
	}
}

// A sorted snapshot of every business table, not only row counts. Querying the
// trusted baseline prevents dynamic fixture names from becoming SQL identifiers.
func (f *catalogSQLFixture) snapshot() [32]byte {
	f.t.Helper()
	tables := []string{}
	for table := range f.baseline.Tables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	content := []string{}
	for _, table := range tables {
		rows, e := f.writer.QueryContext(f.ctx, "SELECT * FROM `"+table+"`")
		if e != nil {
			f.t.Fatal("catalog snapshot query failed")
		}
		cols, e := rows.Columns()
		if e != nil {
			f.t.Fatal(e)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			args := make([]any, len(cols))
			for i := range values {
				args[i] = &values[i]
			}
			if e = rows.Scan(args...); e != nil {
				f.t.Fatal(e)
			}
			b, e := json.Marshal(values)
			if e != nil {
				f.t.Fatal(e)
			}
			content = append(content, table+":"+string(b))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			f.t.Fatal(e)
		}
	}
	sort.Strings(content)
	return sha256.Sum256([]byte(strings.Join(content, "\n")))
}

func catalogFrames(t *testing.T) []catalogSQLFrame {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-catalog-sql.mjs")
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
	frames := []catalogSQLFrame{}
	for {
		var v catalogSQLFrame
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		frames = append(frames, v)
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required source SQLite/router oracle failed")
	}
	if len(frames) != 12 {
		t.Fatal("catalog source frames missing")
	}
	return frames
}

func catalogHandler(t *testing.T, db *sql.DB) (http.Handler, map[string]string) {
	t.Helper()
	m, e := auth.NewManager("CI_catalog_synthetic_session_secret_only_317", map[string]string{"eugene": "CI_pw", "demo": "CI_pw", "nastya": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "demo", "nastya"} {
		u, ok := m.Authenticate(login, "CI_pw")
		if !ok {
			t.Fatal("missing synthetic user")
		}
		tokens[u.Name], e = m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
	}
	repo := catalog.Repository{DB: db}
	return httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, Catalog: repo, Compact: repo}), tokens
}
func catalogRequest(h http.Handler, tokens map[string]string, method, url, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	if user != "" {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMariaDBCatalogReadSourceDisposable(t *testing.T) {
	f := openCatalogSQL(t)
	h, tokens := catalogHandler(t, f.reader)
	count := 0
	for _, v := range catalogFrames(t) {
		t.Run(v.Name, func(t *testing.T) {
			f.seed(v)
			before := f.snapshot()
			for i, r := range v.Requests {
				w := catalogRequest(h, tokens, r.Method, r.URL, r.User)
				count++
				// Compare the JSON structure without depending on Go's object key order.
				// Lone UTF16/search cap is also checked by the exact pure source gates.
				var a, b any
				ea := json.Unmarshal(w.Body.Bytes(), &a)
				eb := json.Unmarshal([]byte(r.Body), &b)
				ja, _ := json.Marshal(a)
				jb, _ := json.Marshal(b)
				if w.Code != r.Status || ea != nil || eb != nil || string(ja) != string(jb) {
					t.Fatalf("response %d %s %s/%s: status %d/%d\nGo %s\nsource %s", i, r.Method, r.URL, r.User, w.Code, r.Status, w.Body.String(), r.Body)
				}
				if strings.Contains(w.Body.String(), "CI_PRIVATE_") {
					t.Fatal("private field leaked")
				}
			}
			if f.snapshot() != before {
				t.Fatal("GET/negative method changed business data")
			}
		})
	}
	if count != 2592 {
		t.Fatal("missing catalog HTTP responses", count)
	}
	t.Logf("PASS: %d actual source SQLite / readonly MariaDB HTTP responses; all 72 table contents unchanged", count)
}

func TestMariaDBCatalogReadFailuresDisposable(t *testing.T) {
	f := openCatalogSQL(t)
	v := catalogFrames(t)[0]
	f.seed(v)
	h, tokens := catalogHandler(t, f.reader)
	for _, tc := range []struct {
		name, sql string
		args      []any
	}{
		{"corrupt catalog", "UPDATE app_catalog SET data_json='not JSON' WHERE id=1", nil},
		{"duplicate identity", "INSERT INTO company_identity(id,company_name) VALUES(99,'CI A')", nil},
		{"duplicate personal state", "INSERT INTO company_user_state(user_name,company_name) VALUES('Eugene','CI A')", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.seed(v)
			if _, e := f.writer.ExecContext(f.ctx, tc.sql, tc.args...); e != nil {
				t.Fatal("synthetic corruption setup failed")
			}
			before := f.snapshot()
			w := catalogRequest(h, tokens, "GET", "/api/companies/list", "Eugene")
			if w.Code != 503 || strings.Contains(w.Body.String(), "SQL") {
				t.Fatalf("unsafe storage response %d %s", w.Code, w.Body.String())
			}
			if f.snapshot() != before {
				t.Fatal("failed read wrote data")
			}
		})
	}
	f.seed(v)
	// A missing migrated identity is represented as null, never lazily recreated
	// by GET. Source comparisons above deliberately use fully prepared identities.
	if _, e := f.writer.ExecContext(f.ctx, "DELETE FROM company_identity WHERE id=11"); e != nil {
		t.Fatal(e)
	}
	before := f.snapshot()
	w := catalogRequest(h, tokens, "GET", "/api/companies/list", "Eugene")
	var missing struct {
		Companies []struct {
			Name string
			ID   *int64
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &missing); e != nil || w.Code != 200 {
		t.Fatal("missing identity read failed")
	}
	found := false
	for _, row := range missing.Companies {
		if row.Name == "CI A" {
			found = true
			if row.ID != nil {
				t.Fatal("missing identity acquired an ID")
			}
		}
	}
	if !found || f.snapshot() != before {
		t.Fatal("GET recreated missing identity or changed data")
	}
	f.seed(v)
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, e := (catalog.Repository{DB: f.reader}).ListEntities(ctx, "Eugene", catalog.CompanyType, catalog.ListView); e == nil {
		t.Fatal("cancelled read succeeded")
	}
	if e := f.reader.Close(); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/companies/list", "/api/agencies/detail?name=CI%20agency", "/api/companies/search?q=CI"} {
		w := catalogRequest(h, tokens, "GET", path, "Eugene")
		if w.Code != 503 {
			t.Fatal("closed storage did not fail closed")
		}
	}
	h, tokens = catalogHandler(t, nil)
	if w := catalogRequest(h, tokens, "GET", "/api/companies/list", "Eugene"); w.Code != 503 {
		t.Fatal("unconfigured DB did not fail closed")
	}
	t.Log("PASS: corruption, target-only duplicate ambiguity, cancellation and storage errors stay private")
}
