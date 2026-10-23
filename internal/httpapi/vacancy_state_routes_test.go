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

type flagProbe struct {
	calls int
	user  string
	id    int64
	key   string
	value bool
	err   error
}

func (p *flagProbe) SetUserFlag(_ context.Context, user string, id int64, key string, val bool) (vacancies.UserFlags, error) {
	p.calls++
	p.user = user
	p.id = id
	p.key = key
	p.value = val
	if p.err != nil {
		return vacancies.UserFlags{}, p.err
	}
	return vacancies.UserFlags{VacancyID: id, Favorite: key == "favorite" && val, Hidden: key == "hidden" && val}, nil
}
func TestVacancyFlagWriteRequiresValidSessionAndOneBoolean(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-secret", "demo": "demo-pass"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "demo-pass")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	probe := &flagProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyFlags: probe})
	call := func(path, body, origin, site, ctype string, authed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", path, strings.NewReader(body))
		if authed {
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
	if w := call("/api/vacancies/4/state", `{"favorite":true}`, "", "", "application/json", false); w.Code != 401 || probe.calls != 0 {
		t.Fatalf("missing auth %d", w.Code)
	}
	for _, tc := range []struct {
		path, body, origin, site, ctype string
		want                            int
	}{
		{"/api/vacancies/4/state", `{"favorite":true}`, "https://malicious.example", "", "application/json", 403},
		{"/api/vacancies/4/state", `{"favorite":true}`, "", "cross-site", "application/json", 403},
		{"/api/vacancies/0/state", `{"favorite":true}`, "", "", "application/json", 400},
		{"/api/vacancies/nope/state", `{"favorite":true}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":true,"hidden":false}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"relevance":99}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":1}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":true,"favorite":false}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":true}`, "", "", "application/jsonp", 400},
		{"/api/vacancies/4/state", `{"favorite":"true"}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":true}{"hidden":true}`, "", "", "application/json", 400},
		{"/api/vacancies/4/state", `{"favorite":true}`, "", "", "text/plain", 400},
		{"/api/vacancies/4/state", strings.Repeat("x", 5000), "", "", "application/json", 400},
	} {
		w := call(tc.path, tc.body, tc.origin, tc.site, tc.ctype, true)
		if w.Code != tc.want || probe.calls != 0 {
			t.Fatalf("invalid %q: HTTP %d calls %d", tc.body, w.Code, probe.calls)
		}
	}
	w := call("/api/vacancies/4/state", `{"hidden":true}`, "http://example.com", "same-origin", "application/json", true)
	if w.Code != 200 || probe.calls != 1 || probe.user != "Demo" || probe.id != 4 || probe.key != "hidden" || !probe.value || !strings.Contains(w.Body.String(), `"hidden":true`) {
		t.Fatalf("state %d %s %+v", w.Code, w.Body.String(), probe)
	}
	probe.err = vacancies.ErrNotFound
	if w = call("/api/vacancies/4/state", `{"favorite":false}`, "", "", "application/json", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	probe.err = errors.New("DB_PASSWORD=do-not-expose")
	if w = call("/api/vacancies/4/state", `{"favorite":false}`, "", "", "application/json", true); w.Code != 503 || strings.Contains(w.Body.String(), "DB_PASSWORD") {
		t.Fatalf("server detail leak %s", w.Body.String())
	}
}
