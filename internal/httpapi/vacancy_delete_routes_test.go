package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type deleteProbe struct {
	calls  int
	id     int64
	result vacancies.DeleteResult
	err    error
}

func (p *deleteProbe) DeleteManual(_ context.Context, id int64) (vacancies.DeleteResult, error) {
	p.calls++
	p.id = id
	return p.result, p.err
}
func TestManualVacancyDeleteRoute(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "adminpassword", "demo": "demopassword"})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := m.Authenticate("eugene", "adminpassword")
	demo, _ := m.Authenticate("demo", "demopassword")
	a, _ := m.Issue(admin, time.Now())
	d, _ := m.Issue(demo, time.Now())
	probe := &deleteProbe{result: vacancies.DeleteResult{OK: true, Archived: true, CompanyName: "Example"}}
	h := NewHandlerWithOptions(nil, Options{Auth: m, VacancyDeletes: probe})
	request := func(path, token, origin, site string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("DELETE", "http://example.com"+path, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		label, path, token, origin, site string
		status                           int
	}{
		{"unauth", "/api/vacancies/10", "", "", "", 401},
		{"non-admin", "/api/vacancies/10", d, "", "", 403},
		{"cross origin", "/api/vacancies/10", a, "https://attacker.example", "", 403},
		{"cross site", "/api/vacancies/10", a, "", "cross-site", 403},
		{"zero id", "/api/vacancies/0", a, "", "", 400},
		{"bad id", "/api/vacancies/hi", a, "", "", 400},
		{"overflow", "/api/vacancies/999999999999999999999", a, "", "", 400},
	} {
		t.Run(tc.label, func(t *testing.T) {
			w := request(tc.path, tc.token, tc.origin, tc.site)
			if w.Code != tc.status || probe.calls != 0 {
				t.Fatalf("code=%d calls=%d", w.Code, probe.calls)
			}
		})
	}
	w := request("/api/vacancies/10", a, "http://example.com", "same-origin")
	if w.Code != 200 || probe.calls != 1 || probe.id != 10 || !strings.Contains(w.Body.String(), `"archived":true`) {
		t.Fatalf("%d %s %+v", w.Code, w.Body.String(), probe)
	}
	for _, tc := range []struct {
		err  error
		code int
	}{
		{vacancies.ErrNotFound, 404}, {vacancies.ErrAgencySalaryProjectionPending, 503}, {errors.New("DSN=private-connection"), 503},
	} {
		probe.err = tc.err
		w = request("/api/vacancies/10", a, "", "")
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private-connection") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
