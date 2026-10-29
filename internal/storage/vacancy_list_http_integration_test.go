//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"io"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fixedVacancyHTTPList struct {
	repo  vacancies.Repository
	now   time.Time
	calls int
}

// Только тестовый clock seam: настоящий handler и Repository, без зависимости от даты CI.
func (r *fixedVacancyHTTPList) List(ctx context.Context, user string, period *vacancies.Period, _ time.Time) (vacancies.ListResponse, error) {
	r.calls++
	return r.repo.List(ctx, user, period, r.now)
}
func TestMariaDBVacancyListHTTPSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"company_applications", "company_vacancy_user_state", "company_vacancies", "company_crm", "app_catalog"}
	for _, table := range append(append([]string{}, tables...), "vacancy_roles", "vacancy_telegram_sources") {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("vacancy HTTP fixture requires empty disposable namespace")
		}
	}
	cleanup := func(ctx context.Context) {
		for _, table := range tables {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("vacancy HTTP cleanup failed")
			}
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanup(ctx)
	})
	m, err := auth.NewManager("CI_SYNTHETIC_LIST_READ_SECRET_342", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
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
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-list-http.mjs")
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
	var before [32]byte
	for {
		var frame struct {
			Name, Path, User, Query, Origin, ResponseBody string
			NoDB, BadCookie, Empty                        bool
			Now                                           int64
			Status                                        int
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if !prepared || empty != frame.Empty {
			cleanup(f.ctx)
			if t.Failed() {
				return
			}
			f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[],"agencies":[]}`)
			if !frame.Empty {
				f.exec("INSERT INTO company_crm(company_name) VALUES('alpha'),('Beta')")
				f.exec("INSERT INTO company_vacancies(id,company_name,url,title,published_at,created_at) VALUES(1,'alpha','https://example.invalid/go342/1','CI period','2026-10-28','2026-10-28 10:00:00'),(2,'Beta','https://example.invalid/go342/2','CI period','2026-07-01','2026-07-01 10:00:00')")
				f.exec("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES('Demo',1,11,1,0),('Nastya',1,22,0,1)")
				f.exec("INSERT INTO company_applications(user_name,company_name,vacancy_id) VALUES('Demo','alpha',NULL),('Nastya','Beta',NULL)")
			}
			prepared, empty = true, frame.Empty
			before = dashboardFingerprint(f)
		}
		t.Run(frame.Name, func(t *testing.T) {
			probe := &fixedVacancyHTTPList{repo: vacancies.Repository{DB: f.reader}, now: time.UnixMilli(frame.Now)}
			var repo httpapi.VacancyLister = probe
			if frame.NoDB {
				repo = nil
			}
			h := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, Vacancies: repo})
			req := httptest.NewRequest("GET", "https://example.invalid"+frame.Path+"?"+frame.Query, nil)
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
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			if out.Code != frame.Status {
				t.Fatalf("source status differs: got %d want %d", out.Code, frame.Status)
			}
			var got, want any
			if json.Valid([]byte(frame.ResponseBody)) {
				if json.Unmarshal(out.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(frame.ResponseBody), &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("native full source DTO differs: got %s want %s", out.Body.String(), frame.ResponseBody)
				}
			} else if strings.TrimSuffix(out.Body.String(), "\n") != frame.ResponseBody {
				t.Fatal("source text differs")
			}
			wantCalls := 0
			if frame.Status == 200 {
				wantCalls = 1
			}
			if probe.calls != wantCalls {
				t.Fatal("source read precedence differs")
			}
			if before != dashboardFingerprint(f) {
				t.Fatal("vacancy HTTP read changed one of 72 tables")
			}
		})
	}
	if cmd.Wait() != nil || count != 176 {
		t.Fatal("required vacancy HTTP cases missing", count)
	}
	t.Log("PASS: source vacancy HTTP176 full DTO/status cases, period/query/users/noDB, SELECT-only and 72 tables unchanged")
}
