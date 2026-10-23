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

type createProbe struct {
	calls int
	user  string
	input vacancies.Input
	err   error
}

func (p *createProbe) CreateManual(_ context.Context, user string, input vacancies.Input) ([]vacancies.CompanyVacancy, error) {
	p.calls++
	p.user = user
	p.input = input
	if p.err != nil {
		return nil, p.err
	}
	return []vacancies.CompanyVacancy{{Vacancy: vacancies.Vacancy{ID: 17, CompanyName: "Example", Title: input.Title}}}, nil
}

func TestManualVacancyCreateRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("x", 64), map[string]string{"eugene": "admin-secret", "demo": "demo-secret"})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := manager.Authenticate("eugene", "admin-secret")
	demo, _ := manager.Authenticate("demo", "demo-secret")
	adminToken, _ := manager.Issue(admin, time.Now())
	demoToken, _ := manager.Issue(demo, time.Now())
	probe := &createProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyCreates: probe})
	valid := `{"companyName":"Example","url":"https://example.com/post/3","title":"Lead","descriptionMd":"Details","level":"Head","roleIds":[2]}`
	call := func(token, body, origin, site, ct string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.com/api/vacancies", strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	cases := []struct {
		label, token, body, origin, site, ctype string
		status                                  int
	}{
		{"unauthorized", "", valid, "", "", "application/json", 401},
		{"not admin", demoToken, valid, "", "", "application/json", 403},
		{"wrong origin", adminToken, valid, "https://evil.example", "", "application/json", 403},
		{"fetch site", adminToken, valid, "", "cross-site", "application/json", 403},
		{"wrong MIME", adminToken, valid, "", "", "text/plain", 400},
		{"non-object", adminToken, `[]`, "", "", "application/json", 400},
		{"empty", adminToken, ``, "", "", "application/json", 400},
		{"trailing", adminToken, valid + `{}`, "", "", "application/json", 400},
		{"personal letter", adminToken, `{"coverLetterScope":"personal"}`, "", "", "application/json", 400},
		{"size limit", adminToken, strings.Repeat(" ", 256<<10) + valid, "", "", "application/json", 400},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			w := call(tc.token, tc.body, tc.origin, tc.site, tc.ctype)
			if w.Code != tc.status || probe.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, probe.calls, w.Body.String())
			}
		})
	}
	w := call(adminToken, valid, "http://example.com", "same-origin", "application/json")
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"vacancies"`) || probe.calls != 1 || probe.user != "Eugene" || probe.input.CompanyName != "Example" {
		t.Fatalf("create %d %s %+v", w.Code, w.Body.String(), probe)
	}
	for _, tc := range []struct {
		err  error
		code int
	}{
		{vacancies.ErrInvalidVacancyInput, 400}, {vacancies.ErrInvalidRole, 400}, {vacancies.ErrInvalidChannel, 400},
		{vacancies.ErrInvalidOwner, 404}, {vacancies.ErrDuplicateURL, 400},
		{vacancies.ErrDuplicateMessage, 400}, {vacancies.ErrWriteBusy, 503}, {errors.New("password=db-secret"), 503},
	} {
		probe.err = tc.err
		w = call(adminToken, valid, "", "", "application/json")
		if w.Code != tc.code || strings.Contains(w.Body.String(), "db-secret") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/vacancies", strings.NewReader(valid))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	NewHandlerWithOptions(nil, Options{Auth: manager}).ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("unavailable route must remain session guarded", w.Code)
	}
}
