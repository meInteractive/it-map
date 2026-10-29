//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/roles"
)

type roleReadHTTPFrame struct {
	Name, Path, User, Origin, ResponseBody string
	Empty, NoDB, BadCookie                 bool
	Reads, Status                          int
	Seeds                                  []roles.Role
	Assignments                            []struct{ Vacancy, Role int64 }
}
type countedRoleRead struct {
	repo  roles.Repository
	calls int
}

func (r *countedRoleRead) Read(ctx context.Context) (roles.Catalog, error) {
	r.calls++
	return r.repo.Read(ctx)
}

func TestMariaDBRoleReadHTTPSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"vacancy_role_assignments", "vacancy_roles", "company_vacancies", "company_crm"}
	for _, table := range append(append([]string{}, tables...), "company_applications", "company_vacancy_user_state") {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("role HTTP fixture requires empty disposable namespace")
		}
	}
	clearRoles := func(ctx context.Context) {
		for _, sql := range []string{"DELETE FROM vacancy_role_assignments", "DELETE FROM vacancy_roles ORDER BY id DESC"} {
			if _, err := f.db.ExecContext(ctx, sql); err != nil {
				t.Error("role HTTP cleanup failed")
			}
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		clearRoles(ctx)
		for _, table := range tables[2:] {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("role HTTP parent cleanup failed")
			}
		}
	})
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI GO341 employer')")
	f.exec("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'CI GO341 employer','https://example.invalid/go341/1','CI role usage'),(2,'CI GO341 employer','https://example.invalid/go341/2','CI role usage')")
	m, err := auth.NewManager("CI_SYNTHETIC_ROLE_READ_SECRET_341", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(login, "CI_pw")
		token, err := m.Issue(u, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		tokens[u.Name] = token
	}
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-role-read-http.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	prepared, empty := false, false
	for {
		var frame roleReadHTTPFrame
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if !prepared || empty != frame.Empty {
			clearRoles(f.ctx)
			if t.Failed() {
				return
			}
			for _, r := range frame.Seeds {
				f.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order) VALUES(?,?,?,?,?)", r.ID, r.StableKey, r.Name, r.ParentID, r.SortOrder)
			}
			for _, a := range frame.Assignments {
				f.exec("INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)", a.Vacancy, a.Role)
			}
			prepared, empty = true, frame.Empty
		}
		t.Run(frame.Name, func(t *testing.T) {
			probe := &countedRoleRead{repo: roles.Repository{DB: f.reader}}
			var repo httpapi.RoleReader = probe
			if frame.NoDB {
				repo = nil
			}
			handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, Roles: repo})
			req := httptest.NewRequest("GET", "https://example.invalid"+frame.Path, nil)
			if frame.Origin != "" {
				req.Header.Set("Origin", frame.Origin)
			}
			if frame.User != "" {
				token := tokens[frame.User]
				if frame.BadCookie {
					token = "CI_INVALID_COOKIE"
				}
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			}
			before := dashboardFingerprint(f)
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			if out.Code != frame.Status {
				t.Fatalf("source status differs: got %d want %d", out.Code, frame.Status)
			}
			var got, want any
			if json.Valid([]byte(frame.ResponseBody)) {
				if json.Unmarshal(out.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(frame.ResponseBody), &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("native source role DTO differs: got %s want %s", out.Body.String(), frame.ResponseBody)
				}
			} else if strings.TrimSuffix(out.Body.String(), "\n") != frame.ResponseBody {
				t.Fatalf("native source text differs: %q", out.Body.String())
			}
			if probe.calls != frame.Reads {
				t.Fatal("source authorization/path/storage precedence differs")
			}
			if before != dashboardFingerprint(f) {
				t.Fatal("role HTTP read changed one of 72 tables")
			}
		})
	}
	if cmd.Wait() != nil || count != 166 {
		t.Fatal("required role HTTP source corpus missing", count)
	}
	t.Log("PASS: source role HTTP 166 cases, tree/descendants/IDs/3 users/anonymous/no DB, SELECT-only and 72 tables unchanged")
}
