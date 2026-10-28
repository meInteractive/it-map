package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

type catalogProbe struct {
	Calls   int
	User    string
	Kind    catalog.EntityType
	View    catalog.View
	Name    string
	ID      float64
	Failure error
}

func (p *catalogProbe) ListEntities(_ context.Context, user string, kind catalog.EntityType, view catalog.View) (json.RawMessage, error) {
	p.Calls++
	p.User = user
	p.Kind = kind
	p.View = view
	if p.Failure != nil {
		return nil, p.Failure
	}
	return json.RawMessage(`[{"favorite":false,"hidden":true,"id":9,"name":"Агентство Т"}]`), nil
}
func (p *catalogProbe) EntityDetail(_ context.Context, user string, kind catalog.EntityType, id float64, name string) (json.RawMessage, error) {
	p.Calls++
	p.User = user
	p.Kind = kind
	p.ID = id
	p.Name = name
	if p.Failure != nil {
		return nil, p.Failure
	}
	return json.RawMessage(`{"name":"Агентство Т","vacancyCount":4}`), nil
}
func TestCompactRoutesAccessAndContract(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("k", 64), map[string]string{"eugene": "password", "demo": "demopw"})
	if err != nil {
		t.Fatal(err)
	}
	user, ok := m.Authenticate("demo", "demopw")
	if !ok {
		t.Fatal("demo missing")
	}
	token, err := m.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := &catalogProbe{}
	search := &testCatalog{}
	handler := NewHandlerWithOptions(nil, Options{Auth: m, Compact: p, Catalog: search})
	send := func(method, path string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if authorized {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/api/companies/list", "/api/companies/map", "/api/agencies/list", "/api/agencies/map", "/api/companies/detail?name=a", "/api/agencies/9"} {
		w := send("GET", path, false)
		if w.Code != 401 {
			t.Fatalf("%s unauthenticated: %d %s", path, w.Code, w.Body.String())
		}
	}
	if p.Calls != 0 {
		t.Fatal("repository queried without auth")
	}
	if w := send("GET", "/api/companies/list", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"companies":[{"favorite":false,"hidden":true,"id":9,"name":"Агентство Т"}]`) {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	if p.Kind != catalog.CompanyType || p.View != catalog.ListView || p.User != "Demo" {
		t.Fatalf("wrong personal scope: %+v", p)
	}
	if w := send("GET", "/api/agencies/map", true); w.Code != 200 || p.Kind != catalog.AgencyType || p.View != catalog.MapView {
		t.Fatalf("map %d %+v", w.Code, p)
	}
	name := "Тестовое агентство"
	if w := send("GET", "/api/agencies/detail?name="+url.QueryEscape(name), true); w.Code != 200 || p.Name != name || p.ID != 0 || p.Kind != catalog.AgencyType {
		t.Fatalf("detail by name %d %+v", w.Code, p)
	}
	if w := send("GET", "/api/companies/123", true); w.Code != 200 || p.ID != 123 || p.Kind != catalog.CompanyType {
		t.Fatalf("detail by ID %d %+v", w.Code, p)
	}
	// Unported endpoints do NOT become entity lookups or return success.
	before := p.Calls
	for _, path := range []string{"/api/companies/recalculate-rating", "/api/agencies/unported", "/api/companies/import"} {
		if w := send("GET", path, true); w.Code != 503 {
			t.Fatalf("unported GET %s %d", path, w.Code)
		}
	}
	if p.Calls != before {
		t.Fatalf("unported endpoints hit DB: %d -> %d", before, p.Calls)
	}
	if w := send("GET", "/api/agencies/search?q=Hello", true); w.Code != 200 || search.query != "Hello" {
		t.Fatalf("legacy agency search alias %d", w.Code)
	}
	if w := send("POST", "/api/companies/list", true); w.Code != 405 {
		t.Fatalf("POST list status/body: %d %s", w.Code, w.Body.String())
	}
}
func TestCompactRoutesFailuresStayPrivate(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("c", 40), map[string]string{"eugene": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("eugene", "pw")
	token, _ := m.Issue(u, time.Now())
	p := &catalogProbe{Failure: errors.New("DB password=top-secret")}
	h := NewHandlerWithOptions(nil, Options{Auth: m, Compact: p})
	call := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/companies/list", "/api/agencies/33"} {
		w := call(path)
		if w.Code != 503 || strings.Contains(w.Body.String(), "top-secret") {
			t.Fatalf("leaked error: %d %s", w.Code, w.Body.String())
		}
	}
	p.Failure = catalog.ErrNotFound
	if w := call("/api/agencies/detail?name=missing"); w.Code != 404 {
		t.Fatalf("missing detail %d %s", w.Code, w.Body.String())
	}
	if w := call("/api/companies/99999999999999999999999999999"); w.Code != 404 {
		t.Fatalf("unexpected huge ID %d", w.Code)
	}
}
