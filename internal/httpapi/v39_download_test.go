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
	"itmap/internal/backup"
)

type exportProbe struct {
	calls int
	err   error
}

func (p *exportProbe) Export(context.Context) (backup.Download, error) {
	p.calls++
	return backup.Download{Bytes: []byte(`{"createdAt":"2026-10-27T00:00:00.000Z"}`), CreatedAt: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)}, p.err
}
func TestV39DownloadContract(t *testing.T) {
	manager, e := auth.NewManager(strings.Repeat("x", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_MEMBER"})
	if e != nil {
		t.Fatal(e)
	}
	store := &exportProbe{}
	call := func(user, method string, enabled bool) *httptest.ResponseRecorder {
		opts := Options{Auth: manager}
		if enabled {
			opts.BackupExports = store
		}
		r := httptest.NewRequest(method, "https://itmap.test/api/backup", nil)
		if user != "" {
			password := "CI_ADMIN"
			if user == "demo" {
				password = "CI_MEMBER"
			}
			u, _ := manager.Authenticate(user, password)
			token, err := manager.Issue(u, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, opts).ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct {
		user, method string
		ready        bool
		status       int
	}{{"", "GET", true, 401}, {"demo", "GET", true, 403}, {"demo", "GET", false, 403}, {"eugene", "GET", false, 503}, {"eugene", "DELETE", true, 405}, {"eugene", "POST", true, 503}} {
		if w := call(c.user, c.method, c.ready); w.Code != c.status {
			t.Fatalf("%s %s: %d", c.user, c.method, w.Code)
		}
	}
	if store.calls != 0 {
		t.Fatal("denied requests reached exporter")
	}
	w := call("eugene", "GET", true)
	if w.Code != 200 || w.Header().Get("Content-Disposition") != `attachment; filename="it-employers-backup-2026-10-27.json"` || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("download contract/headers differ")
	}
	store.err = errors.New("CI_SQL_DETAILS_NOT_PUBLIC")
	w = call("eugene", "GET", true)
	if w.Code != 503 || strings.Contains(w.Body.String(), store.err.Error()) || w.Header().Get("Content-Disposition") != "" {
		t.Fatal("failed export leaked SQL or returned partial attachment")
	}
	store.err = &backup.InputError{Status: 413, Message: "Полный бэкап превышает 25 МБ."}
	if w = call("eugene", "GET", true); w.Code != 400 {
		t.Fatal("legacy download validation failures must be 400")
	}
}
