//go:build mariadb && v39restore && restorehttp

package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/httpapi"
)

func TestMariaDBV39RestoreHTTPDisposable(t *testing.T) {
	f := openRestoreFixture(t)
	manager, e := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_RESTORE_PASSWORD", "demo": "CI_RESTORE_PASSWORD"})
	if e != nil {
		t.Fatal("auth setup failed")
	}
	opts := httpapi.Options{DB: f.db, Auth: manager, BackupExports: backup.Repository{DB: f.db}, BackupRestores: backup.Repository{DB: f.db}}
	handler := httpapi.NewHandlerWithOptions(nil, opts)
	cookies := map[string]*http.Cookie{}
	for _, login := range []string{"eugene", "demo"} {
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(url.Values{"login": {login}, "password": {"CI_RESTORE_PASSWORD"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 303 {
			t.Fatal("actual login failed")
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == auth.CookieName && cookie.Secure && cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode {
				cookies[login] = cookie
			}
		}
		if cookies[login] == nil {
			t.Fatal("signed protected cookie missing")
		}
	}
	call := func(user, method, path, origin string, body io.Reader, length int64, want int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, "https://itmap.test"+path, body).WithContext(f.ctx)
		req.ContentLength = length
		if length == -1 {
			req.TransferEncoding = []string{"chunked"}
		}
		if user != "" {
			req.AddCookie(cookies[user])
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != want || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("%s %s status=%d want=%d", method, path, w.Code, want)
		}
		if want != 200 {
			var value map[string]any
			if json.Unmarshal(w.Body.Bytes(), &value) != nil || value["error"] == nil {
				t.Fatal("error contract differs")
			}
		}
		return w.Body.Bytes()
	}
	before, gen := f.snapshot(), f.generation()
	var counter int64
	if e = f.db.QueryRowContext(f.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&counter); e != nil {
		t.Fatal("counter read failed")
	}
	for _, path := range []string{"/api/backup", "/api/backup/validate"} {
		call("", "POST", path, "", bytes.NewReader(f.body), int64(len(f.body)), 401)
		call("demo", "POST", path, "", bytes.NewReader(f.body), int64(len(f.body)), 403)
		call("eugene", "POST", path, "https://cross.invalid", bytes.NewReader(f.body), int64(len(f.body)), 403)
	}
	call("eugene", "GET", "/api/backup/validate", "", nil, 0, 405)
	response := call("eugene", "POST", "/api/backup/validate", "https://itmap.test", bytes.NewReader(append([]byte{0xef, 0xbb, 0xbf}, f.body...)), -1, 200)
	var summary map[string]json.RawMessage
	if json.Unmarshal(response, &summary) != nil || string(summary["rows"]) != "49" || string(summary["tables"]) != "52" {
		t.Fatal("validate summary differs")
	}
	var counterAfter int64
	if e = f.db.QueryRowContext(f.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&counterAfter); e != nil || counterAfter != counter {
		t.Fatal("validation changed counter")
	}
	call("eugene", "POST", "/api/backup/validate", "", strings.NewReader(`{}`), backup.MaxBytes+1, 413)
	call("eugene", "POST", "/api/backup/validate", "", io.LimitReader(strings.NewReader(strings.Repeat(" ", backup.MaxBytes+1)), backup.MaxBytes+1), -1, 413)
	tampered := bytes.Replace(f.body, []byte("Synthetic company"), []byte("CI tampered company"), 1)
	call("eugene", "POST", "/api/backup", "", bytes.NewReader(tampered), -1, 400)
	if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("validation/denied requests wrote data")
	}
	response = call("eugene", "POST", "/api/backup", "https://itmap.test", bytes.NewReader(f.body), int64(len(f.body)), 200)
	if json.Unmarshal(response, &summary) != nil || string(summary["ok"]) != "true" || string(summary["version"]) != "39" || string(summary["sourceVersion"]) != "39" || string(summary["companies"]) != "1" || string(summary["agencies"]) != "0" || summary["generation"] != nil {
		t.Fatal("legacy restore response differs")
	}
	if gen == f.generation() || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
		t.Fatal("signed admin restore differs")
	}
	before, gen = f.snapshot(), f.generation()
	hook := &restoreFault{prefix: "UPDATE app_runtime_generation SET"}
	pool := restoreFaultPool(f, hook)
	opts.BackupRestores = backup.Repository{DB: pool}
	handler = httpapi.NewHandlerWithOptions(nil, opts)
	response = call("eugene", "POST", "/api/backup", "", bytes.NewReader(f.body), -1, 400)
	if strings.Contains(string(response), "CI_RESTORE_AFTER_SQL") || !hook.fired || gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("HTTP SQL fault leaked or failed rollback")
	}
	opts.BackupRestores = backup.Repository{DB: f.db}
	handler = httpapi.NewHandlerWithOptions(nil, opts)
	call("eugene", "POST", "/api/backup", "", bytes.NewReader(f.body), -1, 200)
	exported := call("eugene", "GET", "/api/backup", "", nil, 0, 200)
	if _, e = backup.ValidateV39(exported); e != nil {
		t.Fatal("restored HTTP download invalid")
	}
}
