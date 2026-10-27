//go:build mariadb && v39restore && historicalrestore

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestMariaDBHistoricalHTTPDisposable(t *testing.T) {
	for _, version := range []int{16, 27, 38} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f, v := openHistoricalFixture(t, version)
			repo := backup.Repository{DB: f.db}
			manager, e := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_HISTORY_PASSWORD", "demo": "CI_HISTORY_PASSWORD"})
			if e != nil {
				t.Fatal("auth unavailable")
			}
			opts := httpapi.Options{DB: f.db, Auth: manager, BackupExports: repo, BackupRestores: repo}
			handler := httpapi.NewHandlerWithOptions(nil, opts)
			cookies := map[string]*http.Cookie{}
			for _, name := range []string{"eugene", "demo"} {
				req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(url.Values{"login": {name}, "password": {"CI_HISTORY_PASSWORD"}}.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", "https://itmap.test")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != 303 {
					t.Fatal("actual historical login failed")
				}
				for _, cookie := range w.Result().Cookies() {
					if cookie.Name == auth.CookieName {
						cookies[name] = cookie
					}
				}
				if cookies[name] == nil {
					t.Fatal("signed cookie missing")
				}
			}
			call := func(user, path, body string, want int) []byte {
				t.Helper()
				req := httptest.NewRequest("POST", "https://itmap.test"+path, strings.NewReader(body)).WithContext(f.ctx)
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
				req.Header.Set("Origin", "https://itmap.test")
				if user != "" {
					req.AddCookie(cookies[user])
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != want || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatalf("historical HTTP %s: %d want %d", path, w.Code, want)
				}
				return w.Body.Bytes()
			}
			before, gen := f.snapshot(), f.generation()
			call("", "/api/backup", v.Body, 401)
			call("demo", "/api/backup", v.Body, 403)
			call("eugene", "/api/backup/validate", v.Body, 200)
			tampered := bytes.Replace([]byte(v.Body), []byte("Synthetic company"), []byte("CI tampered company"), 1)
			call("eugene", "/api/backup", string(tampered), 400)
			if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("historical HTTP validation or denied request wrote data")
			}
			out := call("eugene", "/api/backup", v.Body, 200)
			var summary struct {
				OK                                   bool
				SourceVersion, Version, Tables, Rows int
				Warnings                             []string
			}
			if json.Unmarshal(out, &summary) != nil || !summary.OK || summary.SourceVersion != version || summary.Version != 39 || summary.Tables != 52 || summary.Rows != v.Rows || !reflect.DeepEqual(summary.Warnings, v.Warnings) {
				t.Fatal("historical source HTTP summary differs")
			}
			if gen == f.generation() || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, v.Expected)) {
				t.Fatal("signed historical restore differs from source")
			}
			before, gen = f.snapshot(), f.generation()
			hook := &restoreFault{prefix: "UPDATE app_runtime_generation SET"}
			opts.BackupRestores = backup.Repository{DB: restoreFaultPool(f, hook)}
			handler = httpapi.NewHandlerWithOptions(nil, opts)
			out = call("eugene", "/api/backup", v.Body, 400)
			if !hook.fired || bytes.Contains(out, []byte("CI_RESTORE_AFTER_SQL")) || gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("historical HTTP fault leaked or committed")
			}
		})
	}
}
