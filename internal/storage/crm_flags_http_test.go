//go:build mariadb && crmwrites

package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"itmap/internal/auth"
	"itmap/internal/crm"
	"itmap/internal/httpapi"
)

type crmFlagHTTP struct {
	f       *crmFlagFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openCRMFlagHTTP(f *crmFlagFixture, writer crm.Repository) *crmFlagHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic CRM auth")
	}
	h := &crmFlagHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, CompanyCRM: f.repo, CompanyFlags: f.repo, CompanyFlagWrites: writer})
	for _, login := range []string{"demo", "nastya"} {
		form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_TASK_PASSWORD"}}
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 303 {
			f.t.Fatal("synthetic CRM login failed")
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == auth.CookieName && cookie.Secure && cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode {
				h.cookies[login] = cookie
			}
		}
		if h.cookies[login] == nil {
			f.t.Fatal("login did not issue a protected session cookie")
		}
	}
	return h
}
func (h *crmFlagHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic CRM HTTP payload")
		}
	}
	req := httptest.NewRequest(method, "https://itmap.test"+path, bytes.NewReader(body)).WithContext(h.f.ctx)
	if user != "" {
		req.AddCookie(h.cookies[user])
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" {
		req.Header.Set("Origin", "https://itmap.test")
	}
	for _, header := range headers {
		for key, values := range header {
			req.Header[key] = values
		}
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != want {
		h.f.t.Fatalf("%s %s: status %d, expected %d", method, path, w.Code, want)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		h.f.t.Fatal("CRM API response must be JSON/private/no-store")
	}
	return w.Body.Bytes()
}
func crmFlagJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected CRM API JSON shape")
	}
	return v
}
func (h *crmFlagHTTP) error(data []byte) {
	h.f.t.Helper()
	v := crmFlagJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("CRM API error missing")
	}
}

func TestMariaDBCRMFlagHTTPDisposable(t *testing.T) {
	t.Run("signed HTTP flags readback isolation errors and Unicode", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		h := openCRMFlagHTTP(f, f.repo)
		path := "/api/crm/favorite?company=" + url.QueryEscape(crmFreshCompany)
		h.error(h.call("", "PUT", path, `{"favorite":true}`, 401))
		for _, origin := range []string{"https://cross-site.invalid", "null"} {
			h.error(h.call("demo", "PUT", path, `{"favorite":true}`, 403, http.Header{"Origin": {origin}}))
		}
		for _, bad := range []string{`{"favorite":null}`, `{"favorite":"true"}`, `{"hidden":true}`, `{"favorite":true,"userName":"Nastya"}`, `{"favorite":true,"favorite":false}`, `{} {}`, `[]`} {
			h.error(h.call("demo", "PUT", path, bad, 400))
		}
		h.error(h.call("demo", "PUT", "/api/crm/favorite?company=CI%20nonexistent%20company", `{"favorite":true}`, 404))
		if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crmFreshCompany) != 0 {
			t.Fatal("invalid HTTP writes bootstrapped CRM")
		}
		got := crmFlagJSON[crm.CompanyState](t, h.call("demo", "PUT", path, `{"favorite":true}`, 200))
		if !got.Favorite || got.Hidden || got.History == nil {
			t.Fatal("CRM flag response contract changed")
		}
		readPath := "/api/crm?company=" + url.QueryEscape(crmFreshCompany)
		got = crmFlagJSON[crm.CompanyState](t, h.call("demo", "GET", readPath, nil, 200))
		if !got.Favorite {
			t.Fatal("Unicode company cannot be read after write")
		}
		foreign := crmFlagJSON[crm.CompanyState](t, h.call("nastya", "GET", readPath+"&userName=Demo", nil, 200))
		if foreign.Favorite || foreign.Hidden || foreign.UpdatedAt != nil || len(foreign.History) != 0 {
			t.Fatal("CRM GET trusted foreign owner input")
		}
		names := crmFlagJSON[struct {
			CompanyNames []string `json:"companyNames"`
		}](t, h.call("nastya", "GET", "/api/crm/favorites", nil, 200))
		if len(names.CompanyNames) != 0 {
			t.Fatal("CRM list leaked another owner")
		}
		hiddenPath := "/api/crm/hidden-company?company=" + url.QueryEscape(crmFreshCompany)
		got = crmFlagJSON[crm.CompanyState](t, h.call("demo", "PUT", hiddenPath, `{"hidden":true}`, 200))
		if !got.Favorite || !got.Hidden {
			t.Fatal("HTTP flags lost independent field")
		}
		names = crmFlagJSON[struct {
			CompanyNames []string `json:"companyNames"`
		}](t, h.call("demo", "GET", "/api/crm/hidden-companies", nil, 200))
		if len(names.CompanyNames) != 1 || names.CompanyNames[0] != crmFreshCompany {
			t.Fatal("hidden list missed write")
		}
	})
	t.Run("HTTP hides SQL fault and exposes no partial state", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		repo, fault, _ := f.faultRepo("INSERT INTO company_user_state")
		h := openCRMFlagHTTP(f, repo)
		path := "/api/crm/favorite?company=" + url.QueryEscape(crmFreshCompany)
		data := h.call("demo", "PUT", path, `{"favorite":true}`, 503)
		h.error(data)
		if strings.Contains(string(data), errCRMFlagFault.Error()) {
			t.Fatal("HTTP disclosed SQL fault details")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("HTTP fault never reached SQL")
		}
		if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crmFreshCompany) != 0 || f.state("Demo", crmFreshCompany).Favorite {
			t.Fatal("HTTP failure left partial CRM state")
		}
		got := crmFlagJSON[crm.CompanyState](t, h.call("demo", "PUT", path, `{"favorite":true}`, 200))
		if !got.Favorite {
			t.Fatal("HTTP flag retry failed")
		}
	})
}
