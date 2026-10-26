//go:build mariadb && profilewrites && v39exports

package storage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"itmap/internal/backup"
	"itmap/internal/httpapi"
)

func TestMariaDBV39ExportHTTPDisposable(t *testing.T) {
	t.Run("actual signed admin download members denied and restore remains unwired", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		reader := exportReadOnlyPool(f)
		f.save("Demo", "CI private resume visible only to backup admin", 0)
		h := openProfileHTTP(f.profileResumeFixture, f.repo, func(opts *httpapi.Options) { opts.BackupExports = backup.Repository{DB: reader} })
		h.error(h.call("", "GET", "/api/backup", nil, 401))
		for _, user := range []string{"demo", "nastya"} {
			h.error(h.call(user, "GET", "/api/backup", nil, 403))
			h.error(h.call(user, "POST", "/api/backup", `{}`, 403))
		}
		h.error(h.call("eugene", "DELETE", "/api/backup", nil, 405))
		h.error(h.call("eugene", "POST", "/api/backup", `{}`, 503))
		h.error(h.call("eugene", "POST", "/api/backup", `{}`, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		r := httptest.NewRequest("GET", "https://itmap.test/api/backup?userName=Demo", nil).WithContext(f.ctx)
		r.AddCookie(h.cookies["eugene"])
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Disposition"), `attachment; filename="it-employers-backup-`) || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("backup HTTP status/headers: %d", w.Code)
		}
		p, e := backup.ValidateV39(w.Body.Bytes())
		if e != nil {
			t.Fatal("HTTP download fails v39 validation")
		}
		found := false
		for _, row := range exportTable(t, p, "user_profiles").Rows {
			if string(row["user_name"]) == `"Demo"` {
				found = string(row["resume_md"]) == `"CI private resume visible only to backup admin"`
			}
		}
		if !found {
			t.Fatal("admin export omitted private data")
		}
	})
	t.Run("HTTP late query failure generic and no attachment or partial file", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		hook := &exportIntercept{prefix: "SELECT `user_name`,`resume_md`", fail: true}
		h := openProfileHTTP(f.profileResumeFixture, f.repo, func(opts *httpapi.Options) {
			opts.BackupExports = backup.Repository{DB: interceptedExportPool(f, hook)}
		})
		data := h.call("eugene", "GET", "/api/backup", nil, 503)
		h.error(data)
		if strings.Contains(string(data), errExportQuery.Error()) || strings.Contains(string(data), "storage") || strings.Contains(string(data), "moscow-it-employers-backup") {
			t.Fatal("failed HTTP export leaked details/partial snapshot")
		}
		hook.mu.Lock()
		fired := hook.fired
		hook.mu.Unlock()
		if !fired {
			t.Fatal("HTTP export fault did not reach real SQL")
		}
	})
}
