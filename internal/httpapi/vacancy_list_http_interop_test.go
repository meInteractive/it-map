//go:build legacyinterop

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"itmap/internal/auth"
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

type vacancyListHTTPProbe struct {
	calls  int
	user   string
	period *vacancies.Period
}

func (p *vacancyListHTTPProbe) List(_ context.Context, user string, period *vacancies.Period, now time.Time) (vacancies.ListResponse, error) {
	p.calls++
	p.user = user
	p.period = period
	return vacancies.BuildList(nil, nil, nil, nil, nil, nil, nil, period, now), nil
}
func TestVacancyListHTTPSource(t *testing.T) {
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
	cmd := exec.Command("node", "../../scripts/check-vacancy-list-http.mjs")
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
	for {
		var f struct {
			Name, Path, User, Query, Origin, ResponseBody string
			Period                                        *string
			NoDB, BadCookie, Empty                        bool
			Status                                        int
		}
		err = decoder.Decode(&f)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(f.Name, func(t *testing.T) {
			probe := &vacancyListHTTPProbe{}
			var repo VacancyLister = probe
			if f.NoDB {
				repo = nil
			}
			h := NewHandlerWithOptions(nil, Options{Auth: m, Vacancies: repo})
			req := httptest.NewRequest("GET", "https://example.invalid"+f.Path+"?"+f.Query, nil)
			if f.Origin != "" {
				req.Header.Set("Origin", f.Origin)
			}
			if f.User != "" {
				token := tokens[f.User]
				if f.BadCookie {
					token = "CI_INVALID_COOKIE"
				}
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			}
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			if out.Code != f.Status {
				t.Fatalf("source status differs: got %d want %d", out.Code, f.Status)
			}
			if f.Status == 200 {
				if probe.calls != 1 || probe.user != f.User || (probe.period == nil) != (f.Period == nil) || probe.period != nil && string(*probe.period) != *f.Period {
					t.Fatal("source period presence/first-value/user dispatch differs")
				}
			} else if probe.calls != 0 {
				t.Fatal("source auth/path/DB/period precedence differs")
			}
			// Полные непустые DTO сравнивает native Repository proof; здесь проверяется HTTP dispatch.
			if f.Status != 200 || f.Empty {
				var got, want any
				if json.Valid([]byte(f.ResponseBody)) {
					if json.Unmarshal(out.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(f.ResponseBody), &want) != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("source response differs: got %s want %s", out.Body.String(), f.ResponseBody)
					}
				} else if strings.TrimSuffix(out.Body.String(), "\n") != f.ResponseBody {
					t.Fatal("source text differs")
				}
			}
		})
	}
	if cmd.Wait() != nil || count != 176 {
		t.Fatal("incomplete vacancy list HTTP corpus", count)
	}
}
