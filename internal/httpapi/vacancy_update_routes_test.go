package httpapi

import (
	"context"
	"errors"
	"itmap/internal/auth"
	"itmap/internal/vacancies"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type updateProbe struct {
	calls int
	user  string
	id    int64
	req   vacancies.UpdateRequest
	err   error
}

func (p *updateProbe) UpdateManual(_ context.Context, user string, id int64, req vacancies.UpdateRequest) ([]vacancies.CompanyVacancy, error) {
	p.calls++
	p.user = user
	p.id = id
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	return []vacancies.CompanyVacancy{{Vacancy: vacancies.Vacancy{ID: id, Title: req.Title, CompanyName: req.CompanyName}}}, nil
}

func TestManualVacancyUpdateRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("x", 64), map[string]string{"eugene": "admin-pass", "demo": "demo-pass"})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := manager.Authenticate("eugene", "admin-pass")
	demo, _ := manager.Authenticate("demo", "demo-pass")
	a, _ := manager.Issue(admin, time.Now())
	d, _ := manager.Issue(demo, time.Now())
	probe := &updateProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyUpdates: probe})
	revision := strings.Repeat("a", 64)
	valid := `{"revision":"` + revision + `","companyName":"Example","url":"https://example.com/","title":"Lead","descriptionMd":"Details","level":"Head","roleIds":[3]}`
	call := func(id, token, body, origin, site, ctype string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "http://example.com/api/vacancies/"+id, strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		name, id, token, body, origin, site, ctype string
		want                                       int
	}{
		{"guest", "10", "", valid, "", "", "application/json", 401},
		{"non admin", "10", d, valid, "", "", "application/json", 403},
		{"other origin", "10", a, valid, "https://evil.example", "", "application/json", 403},
		{"fetch site", "10", a, valid, "", "cross-site", "application/json", 403},
		{"bad ID", "0", a, valid, "", "", "application/json", 400},
		{"invalid ID", "x", a, valid, "", "", "application/json", 400},
		{"no MIME", "10", a, valid, "", "", "", 400},
		{"trailing JSON", "10", a, valid + `{}`, "", "", "application/json", 400},
		{"bad version", "10", a, `{"revision":"not-a-token"}`, "", "", "application/json", 428},
		{"missing version", "10", a, `{"title":"hello"}`, "", "", "application/json", 428},
		{"partial letter", "10", a, `{"revision":"` + revision + `","coverLetterMd":"abc"}`, "", "", "application/json", 400},
		{"oversized", "10", a, valid + strings.Repeat(" ", 256<<10), "", "", "application/json", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.id, tc.token, tc.body, tc.origin, tc.site, tc.ctype)
			if w.Code != tc.want || probe.calls != 0 {
				t.Fatalf("status=%d want=%d calls=%d %s", w.Code, tc.want, probe.calls, w.Body.String())
			}
		})
	}
	w := call("10", a, valid, "http://example.com", "same-origin", "application/json")
	if w.Code != 200 || probe.calls != 1 || probe.id != 10 || probe.user != "Eugene" || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"vacancies"`) {
		t.Fatalf("status=%d probe=%+v body=%s", w.Code, probe, w.Body.String())
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{vacancies.ErrVersionConflict, 409}, {vacancies.ErrRevisionRequired, 428}, {vacancies.ErrNotFound, 404},
		{vacancies.ErrInvalidOwner, 404}, {vacancies.ErrDuplicateURL, 400}, {vacancies.ErrLegacyDuplicates, 409},
		{vacancies.ErrAgencySalaryProjectionPending, 503}, {errors.New("private://db-pass"), 503},
	} {
		probe.err = tc.err
		w = call("10", a, valid, "", "", "application/json")
		if w.Code != tc.status || strings.Contains(w.Body.String(), "db-pass") {
			t.Fatalf("status=%d expected=%d response=%s", w.Code, tc.status, w.Body.String())
		}
	}
}
