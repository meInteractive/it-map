//go:build mariadb && schedulerwrites && schedulersettings

package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/parser"
)

func TestMariaDBSchedulerSettingsHTTPDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	m, e := auth.NewManager("CI_SYNTHETIC_SCHEDULER_HTTP_SECRET_324", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(login, "CI_pw")
		tokens[u.Name], e = m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command("node", "../../scripts/check-scheduler-settings-http.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var v struct {
			Name, User, Method, Origin, ContentType, Body string
			NoDB                                          bool
			ContentLength                                 int64
			RequestBody                                   []byte
			Status                                        int
			Fixture                                       settingsSQLCase
			Row                                           map[string]any
		}
		e = decoder.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal("cannot decode required source HTTP response")
		}
		t.Run(v.Name, func(t *testing.T) {
			f.seed(v.Fixture)
			opts := httpapi.Options{Auth: m, SchedulerSettings: parser.SettingsRepository{DB: f.db}}
			if v.NoDB {
				opts.SchedulerSettings = nil
			}
			r := httptest.NewRequest(v.Method, "https://example.invalid/api/scheduler/settings", bytes.NewReader(v.RequestBody))
			r.ContentLength = v.ContentLength
			r.Header.Set("Origin", v.Origin)
			r.Header.Set("Content-Type", v.ContentType)
			if v.User != "" {
				r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[v.User]})
			}
			w := httptest.NewRecorder()
			httpapi.NewHandlerWithOptions(nil, opts).ServeHTTP(w, r)
			var actual, expected any
			if json.Unmarshal(w.Body.Bytes(), &actual) != nil || json.Unmarshal([]byte(v.Body), &expected) != nil || w.Code != v.Status || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("source HTTP status/JSON differs: %d/%d", w.Code, v.Status)
			}
			if diff := settingsRowDifferences(f.row(), v.Row); len(diff) != 0 {
				t.Fatalf("HTTP full-row columns differ: %v", diff)
			}
			var generation string
			if e := f.db.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || generation != f.generation {
				t.Fatal("settings HTTP changed runtime generation")
			}
		})
		count++
	}
	if e = cmd.Wait(); e != nil || count != 70 {
		t.Fatal("required full source HTTP corpus incomplete", count)
	}
	t.Log("PASS: 70 full source router/SQLite responses match Go/MariaDB status/JSON/full row; configuration starts no worker")
}

// Accepted GO-078 duplicate-input and existing Go Sec-Fetch-Site policies are
// deliberately stronger than source JSON.parse/Origin alone, not parity frames.
func TestMariaDBSchedulerSettingsHTTPGuardsDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	f.seed(settingsCases(t)[0])
	m, e := auth.NewManager("CI_SYNTHETIC_SCHEDULER_HTTP_SECRET_324", map[string]string{"eugene": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(u, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	const valid = `{"enabled":true,"revision":7,"dayBudgetUsd":0,"monthBudgetUsd":0}`
	handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerSettings: parser.SettingsRepository{DB: f.db}})
	before := f.row()
	for _, v := range []struct {
		name, body, site string
		status           int
	}{
		{"duplicate enabled", strings.Replace(valid, `"enabled":true`, `"enabled":true,"enabled":false`, 1), "", 400},
		{"duplicate priority", strings.TrimSuffix(valid, "}") + `,"priorities":{"scan_source":1,"scan_source":2}}`, "", 400},
		{"cross site", valid, "cross-site", 403},
		{"same site", valid, "same-site", 403},
	} {
		t.Run(v.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "https://example.invalid/api/scheduler/settings", strings.NewReader(v.body))
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			r.Header.Set("Sec-Fetch-Site", v.site)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != v.status || len(settingsRowDifferences(before, f.row())) != 0 {
				t.Fatal("target input/CSRF guard wrote or returned incorrect status")
			}
		})
	}
	for _, table := range []string{"scheduler_jobs", "scheduler_runs", "scheduler_attempts"} {
		var n int
		if e := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("configuration HTTP must not execute scheduler work")
		}
	}
}
