package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type applicationsProbe struct {
	calls    int
	username string
	id       int64
	err      error
}

func (p *applicationsProbe) ByID(_ context.Context, user string, id int64) (applications.Record, error) {
	p.calls++
	p.username = user
	p.id = id
	if p.err != nil {
		return applications.Record{}, p.err
	}
	return applications.Record{ID: id, CompanyName: "Example", ApplicationSources: []string{"hh"}, History: []applications.StatusChange{}}, nil
}
func (p *applicationsProbe) CRM(_ context.Context, user string) (applications.CRMResponse, error) {
	p.calls++
	p.username = user
	if p.err != nil {
		return applications.CRMResponse{}, p.err
	}
	return applications.CRMResponse{Records: []applications.Record{}, Entities: []applications.Entity{}}, nil
}
func TestReadApplicationEndpoints(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-password")
	token, _ := m.Issue(u, time.Now())
	probe := &applicationsProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: m, Applications: probe})
	call := func(path string, authn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if authn {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/applications/7", "/api/crm/applications"} {
		if w := call(path, false); w.Code != 401 || probe.calls != 0 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/api/applications/0", "/api/applications/nope", "/api/applications/9223372036854775808"} {
		if w := call(path, true); w.Code != 400 || probe.calls != 0 {
			t.Fatal(path, w.Code)
		}
	}
	if w := call("/api/applications/7", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"record"`) || probe.username != "Demo" || probe.id != 7 {
		t.Fatal(w.Code, w.Body.String(), probe)
	}
	if w := call("/api/crm/applications", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"entities":[]`) || probe.calls != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	probe.err = applications.ErrNotFound
	if w := call("/api/applications/7", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	probe.err = errors.New("database password special-secret")
	for _, path := range []string{"/api/applications/7", "/api/crm/applications"} {
		w := call(path, true)
		if w.Code != 503 || strings.Contains(w.Body.String(), "special-secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Write methods remain explicitly unavailable and behind login.
	for _, path := range []string{"/api/applications/7", "/api/applications"} {
		r := httptest.NewRequest("PUT", path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 503 {
			t.Fatal(path, w.Code)
		}
	}
}
