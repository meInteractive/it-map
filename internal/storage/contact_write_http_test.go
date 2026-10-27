//go:build mariadb && contactwrites

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/contacts"
	"itmap/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type contactHTTP struct {
	f       *contactFixture
	server  *httptest.Server
	cookies map[string]*http.Cookie
}

func openContactHTTP(t *testing.T, f *contactFixture, writer contacts.Repository) *contactHTTP {
	t.Helper()
	manager, e := auth.NewManager(strings.Repeat("c", 64), map[string]string{"eugene": "CI_CHANNEL_PASSWORD", "demo": "CI_CHANNEL_PASSWORD", "nastya": "CI_CHANNEL_PASSWORD"})
	if e != nil {
		t.Fatal("cannot create synthetic channel auth")
	}
	h := &contactHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.server = httptest.NewTLSServer(httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, Contacts: writer}))
	t.Cleanup(h.server.Close)
	client := h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, user := range []string{"eugene", "demo", "nastya"} {
		body := url.Values{"login": {user}, "password": {"CI_CHANNEL_PASSWORD"}}
		req, e := http.NewRequestWithContext(f.ctx, "POST", h.server.URL+"/auth", strings.NewReader(body.Encode()))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", h.server.URL)
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal("synthetic login failed")
		}
		resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatal("synthetic login rejected")
		}
		for _, c := range resp.Cookies() {
			if c.Name == auth.CookieName && c.Secure && c.HttpOnly && c.SameSite == http.SameSiteStrictMode {
				h.cookies[user] = c
			}
		}
		if h.cookies[user] == nil {
			t.Fatal("protected auth cookie missing")
		}
	}
	return h
}
func (h *contactHTTP) call(user, method, path, origin string, body []byte) (int, []byte) {
	h.f.t.Helper()
	req, e := http.NewRequestWithContext(h.f.ctx, method, h.server.URL+path, bytes.NewReader(body))
	if e != nil {
		h.f.t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if user != "" {
		req.AddCookie(h.cookies[user])
	}
	resp, e := h.server.Client().Do(req)
	if e != nil {
		h.f.t.Fatal("channel HTTP request failed")
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(resp.Body)
	if e != nil || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || resp.Header.Get("Cache-Control") != "private, no-store" {
		h.f.t.Fatal("channel response is not private JSON")
	}
	return resp.StatusCode, data
}

func TestMariaDBContactHTTPDisposable(t *testing.T) {
	t.Run("authenticated unchanged source scenario", func(t *testing.T) {
		f := openContactFixture(t)
		h := openContactHTTP(t, f, f.repo)
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			path := "/api/hiring-contacts"
			if method != "POST" {
				path += "/99999"
			}
			for _, v := range []struct {
				user, origin string
				status       int
			}{{"", "", 401}, {"demo", "", 403}, {"nastya", "", 403}, {"eugene", "https://other.invalid", 403}} {
				status, _ := h.call(v.user, method, path, v.origin, []byte(`{"companyName":"CI Contact Employer","position":1,"fullName":"CI forbidden"}`))
				if status != v.status || f.count("SELECT COUNT(*) FROM hiring_contacts") != 0 || f.count("SELECT COUNT(*) FROM company_crm") != 0 {
					t.Fatal("contact auth/origin initialized storage")
				}
			}
		}
		for _, user := range []string{"demo", "nastya"} {
			status, _ := h.call(user, "GET", "/api/hiring-contacts?company="+url.QueryEscape(f.source.Company), "", nil)
			if status != 200 {
				t.Fatal("member contact read denied")
			}
		}
		runContactScenario(t, f, func(method string, id int64, owner string, payload json.RawMessage) (int, []byte) {
			path := "/api/hiring-contacts"
			if method == "GET" {
				path += "?company=" + url.QueryEscape(owner)
			} else if method != "POST" {
				path += fmt.Sprintf("/%d", id)
			}
			if method == "DELETE" {
				payload = []byte("ignored malformed body")
			}
			return h.call("eugene", method, path, h.server.URL, payload)
		})
	})
	t.Run("HTTP late write failure is private and atomic", func(t *testing.T) {
		f := openContactFixture(t)
		c := f.create(f.source.Company, 1)
		before := f.list(f.source.Company)
		fault, db := openMutationFaultPool(t, "UPDATE hiring_contacts")
		h := openContactHTTP(t, f, contacts.Repository{DB: db, CatalogNames: backup.StoredCatalogNames})
		status, body := h.call("eugene", "PUT", fmt.Sprintf("/api/hiring-contacts/%d", c.ID), h.server.URL, []byte(`{"fullName":"CI rollback"}`))
		var reply struct{ Error string }
		if status != 400 || json.Unmarshal(body, &reply) != nil || reply.Error != "Не удалось обновить контакт." || strings.Contains(string(body), "CI_LATE") {
			t.Fatal("contact SQL error leaked or false success")
		}
		fault.mu.Lock()
		fired, commits := fault.fired, fault.commits
		fault.mu.Unlock()
		if !fired || commits != 0 || !reflect.DeepEqual(before, f.list(f.source.Company)) {
			t.Fatal("HTTP contact fault changed storage")
		}
	})
}
