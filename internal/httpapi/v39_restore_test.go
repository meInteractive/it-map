package httpapi

import (
	"context"
	"errors"
	"io"
	"itmap/internal/auth"
	"itmap/internal/backup"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type restoreProbe struct {
	calls int
	err   error
	body  []byte
}

func (p *restoreProbe) ValidateRestore(_ context.Context, b []byte) (backup.RestoreValidation, error) {
	p.calls++
	p.body = b
	return backup.RestoreValidation{Tables: 52, Rows: 7, Warnings: []string{"CI source warning"}}, p.err
}
func (p *restoreProbe) Restore(_ context.Context, b []byte) (backup.RestoreResult, error) {
	p.calls++
	p.body = b
	return backup.RestoreResult{SourceVersion: 39, Rows: 7, Warnings: []string{"CI source warning"}, Catalog: []byte(`{"companies":[{"name":"CI"}],"agencies":[]}`)}, p.err
}

type brokenRestoreBody struct{}

func (brokenRestoreBody) Read([]byte) (int, error) { return 0, errors.New("CI stream failure") }
func TestV39RestoreHTTPContract(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("x", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_MEMBER"})
	if e != nil {
		t.Fatal(e)
	}
	p := &restoreProbe{}
	call := func(user, method, path, origin string, body io.Reader, length int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://itmap.test"+path, body)
		req.ContentLength = length
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if user != "" {
			password := "CI_ADMIN"
			if user == "demo" {
				password = "CI_MEMBER"
			}
			u, _ := m.Authenticate(user, password)
			token, _ := m.Issue(u, time.Now())
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: m, BackupRestores: p, BackupExports: &exportProbe{}}).ServeHTTP(w, req)
		if !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("restore response is cacheable")
		}
		return w
	}
	for _, path := range []string{"/api/backup", "/api/backup/validate"} {
		for _, tc := range []struct {
			user, method, origin string
			status               int
		}{{"", "POST", "", 401}, {"demo", "POST", "", 403}, {"eugene", "POST", "https://cross.invalid", 403}, {"eugene", "DELETE", "", 405}} {
			w := call(tc.user, tc.method, path, tc.origin, strings.NewReader(`{}`), 2)
			if w.Code != tc.status {
				t.Fatalf("auth/origin/method status=%d want=%d", w.Code, tc.status)
			}
		}
		for _, tc := range []struct {
			body   io.Reader
			length int64
			status int
		}{{strings.NewReader(`{`), -1, 400}, {brokenRestoreBody{}, -1, 400}, {strings.NewReader(`{}`), backup.MaxBytes + 1, 413}, {io.LimitReader(zeroRestoreReader{}, backup.MaxBytes+1), -1, 413}} {
			w := call("eugene", "POST", path, "", tc.body, tc.length)
			if w.Code != tc.status {
				t.Fatalf("body status=%d want=%d", w.Code, tc.status)
			}
		}
	}
	if p.calls != 0 {
		t.Fatal("denied/invalid requests reached restore storage")
	}
	for _, path := range []string{"/api/backup", "/api/backup/validate"} {
		w := call("eugene", "POST", path, "https://itmap.test", strings.NewReader("\ufeff{}"), -1)
		if w.Code != 200 || string(p.body) != "{}" || !strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatal("BOM/summary differs")
		}
		p.err = errors.New("CI_SQL_PRIVATE")
		w = call("eugene", "POST", path, "", strings.NewReader(`{}`), 2)
		if w.Code != 400 || strings.Contains(w.Body.String(), "CI_SQL_PRIVATE") {
			t.Fatal("storage error leaked")
		}
		p.err = &backup.InputError{Status: 400, Message: "CI source validation"}
		if w = call("eugene", "POST", path, "", strings.NewReader(`{}`), 2); w.Code != 400 {
			t.Fatal("input error mapping differs")
		}
		p.err = nil
	}
}

type zeroRestoreReader struct{}

func (zeroRestoreReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}
