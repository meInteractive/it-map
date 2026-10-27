//go:build mariadb && channelwrites

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/channels"
	"itmap/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type channelHTTP struct {
	f       *channelFixture
	server  *httptest.Server
	cookies map[string]*http.Cookie
}

func openChannelHTTP(t *testing.T, f *channelFixture, writer channels.Repository, configure ...func(*httpapi.Options)) *channelHTTP {
	t.Helper()
	manager, e := auth.NewManager(strings.Repeat("c", 64), map[string]string{"eugene": "CI_CHANNEL_PASSWORD", "demo": "CI_CHANNEL_PASSWORD", "nastya": "CI_CHANNEL_PASSWORD"})
	if e != nil {
		t.Fatal("cannot create synthetic channel auth")
	}
	h := &channelHTTP{f: f, cookies: map[string]*http.Cookie{}}
	opts := httpapi.Options{Auth: manager, Channels: f.repo, ChannelWrites: writer}
	for _, fn := range configure {
		fn(&opts)
	}
	h.server = httptest.NewTLSServer(httpapi.NewHandlerWithOptions(nil, opts))
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
func (h *channelHTTP) call(user, method, path, origin string, body []byte) (int, []byte) {
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
func TestMariaDBChannelHTTPDisposable(t *testing.T) {
	t.Run("authenticated source scenario with actual SQL", func(t *testing.T) {
		f := openChannelFixture(t)
		h := openChannelHTTP(t, f, f.repo)
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			path := "/api/channels"
			if method != "POST" {
				path += "/99999"
			}
			for _, v := range []struct {
				user, origin string
				status       int
			}{{"", "", 401}, {"demo", "", 403}, {"nastya", "", 403}, {"eugene", "https://other.invalid", 403}} {
				status, _ := h.call(v.user, method, path, v.origin, []byte(`{"name":"CI","username":"@ci_denied"}`))
				if status != v.status || f.count("SELECT COUNT(*) FROM telegram_channels") != 0 {
					t.Fatal("channel auth/origin reached SQL write")
				}
			}
		}
		for _, user := range []string{"demo", "nastya"} {
			status, data := h.call(user, "GET", "/api/channels", "", nil)
			if status != 200 || string(bytes.TrimSpace(data)) != `{"channels":[]}` {
				t.Fatal("member channel read contract changed")
			}
		}
		runChannelScenario(t, f, func(method string, id int64, payload json.RawMessage) (int, []byte) {
			path := "/api/channels"
			if method != "POST" {
				path += fmt.Sprintf("/%d", id)
			}
			if method == "DELETE" {
				payload = []byte("ignored non JSON")
			}
			return h.call("eugene", method, path, h.server.URL, payload)
		})
		f.assertCatalog()
	})
	t.Run("late SQL errors stay private and rollback over HTTP", func(t *testing.T) {
		f := openChannelFixture(t)
		id := f.create("CI unchanged", "@ci_http_before")
		before := f.list()
		fault, db := openMutationFaultPool(t, "UPDATE telegram_channels")
		h := openChannelHTTP(t, f, channels.Repository{DB: db, CatalogSources: backup.StoredCatalogSourceURLs})
		path := fmt.Sprintf("/api/channels/%d", id)
		status, body := h.call("eugene", "PUT", path, h.server.URL, []byte(`{"comment":"CI rolled back"}`))
		var reply struct{ Error string }
		if status != 400 || json.Unmarshal(body, &reply) != nil || reply.Error != "Не удалось обновить канал." || strings.Contains(string(body), "CI_LATE") {
			t.Fatal("HTTP leaked SQL fault or reported success")
		}
		fault.mu.Lock()
		fired, commits := fault.fired, fault.commits
		fault.mu.Unlock()
		if !fired || commits != 0 || mustChannelJSON(before) != mustChannelJSON(f.list()) {
			t.Fatal("HTTP late fault changed persisted channel")
		}
	})
}
