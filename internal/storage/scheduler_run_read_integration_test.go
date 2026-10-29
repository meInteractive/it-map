//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
	"database/sql"
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

type runReadFrame struct {
	Name, Query, User, Origin, Generation, ResponseBody string
	Status                                              int
	NoDB, WrongGeneration                               bool
	Runs                                                []map[string]any
}

func runReadFrames(t *testing.T) []runReadFrame {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-scheduler-run-read.mjs")
	cmd.Stderr = os.Stderr
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source run oracle failed")
	}
	d, out := json.NewDecoder(bytes.NewReader(raw)), []runReadFrame{}
	for {
		var v runReadFrame
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, v)
	}
	if len(out) != 32 {
		t.Fatal("missing run frames")
	}
	return out
}
func seedRunRead(f *settingsSQLFixture, v runReadFrame) {
	// Фиксированное source generation сохраняет source hash run ID. Начальный
	// workflow nonce уже проверен openSettingsSQL; cleanup восстановит его.
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", v.Generation)
	seedDashboard(f, dashboardSQLFrame{Raw: `{"revision":7}`, Favorite: 1, Runs: v.Runs})
	f.exec("UPDATE scheduler_runs SET ready=9007199254740993 WHERE id=?", v.Runs[0]["id"])
	if v.WrongGeneration {
		f.exec("UPDATE scheduler_runs SET generation='22222222-3333-4333-8444-555555555555' WHERE id=?", v.Runs[0]["id"])
	}
}
func runReadHandler(t *testing.T, store httpapi.SchedulerRunReader) (http.Handler, map[string]string) {
	t.Helper()
	m, e := auth.NewManager("CI_SYNTHETIC_RUN_READ_SECRET_331", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(login, "CI_pw")
		token, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		tokens[u.Name] = token
	}
	return httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerRunRead: store}), tokens
}
func TestMariaDBSchedulerRunReadSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	handler, tokens := runReadHandler(t, parser.RunReadRepository{DB: f.reader})
	noDB, _ := runReadHandler(t, nil)
	for _, v := range runReadFrames(t) {
		t.Run(v.Name, func(t *testing.T) {
			seedRunRead(f, v)
			before := dashboardFingerprint(f)
			uri := "https://example.invalid/api/scheduler/run"
			if v.Query != "" {
				uri += "?" + v.Query
			}
			r := httptest.NewRequest("GET", uri, nil)
			r.Header.Set("Origin", v.Origin)
			if v.User != "" {
				r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[v.User]})
			}
			w := httptest.NewRecorder()
			h := handler
			if v.NoDB {
				h = noDB
			}
			h.ServeHTTP(w, r)
			var got, want any
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(v.ResponseBody), &want) != nil || w.Code != v.Status || !reflect.DeepEqual(got, want) {
				t.Fatalf("source run HTTP/DTO differs: %d/%d", w.Code, v.Status)
			}
			if before != dashboardFingerprint(f) {
				t.Fatal("run GET changed one of 72 tables")
			}
		})
	}
	t.Log("PASS: 32 unchanged source router/SQLite run lookup responses vs Go/MariaDB, readonly account, 72-table immutability")
}
func TestMariaDBSchedulerRunReadFaultsDisposable(t *testing.T) {
	requireSettingsCI(t)
	for _, name := range []string{"consistent snapshot", "late run query fault", "cancelled run query", "generation before snapshot", "POST remains unported"} {
		t.Run(name, func(t *testing.T) {
			f := dashboardFixture(t)
			v := runReadFrames(t)[0]
			seedRunRead(f, v)
			before := dashboardFingerprint(f)
			hook := &settingsHook{}
			switch name {
			case "late run query fault":
				hook.query = "SELECT id,generation,`trigger`"
				hook.failQuery = true
			case "generation before snapshot":
				hook.pauseBegin = true
				hook.entered = make(chan struct{})
				hook.release = make(chan struct{})
			case "POST remains unported":
				hook.query = "SELECT generation FROM app_runtime_generation"
				hook.failQuery = true
			default:
				hook.query = "SELECT id,generation,`trigger`"
				hook.entered = make(chan struct{})
				hook.release = make(chan struct{})
			}
			db := settingsFaultPool(f, hook, true)
			handler, tokens := runReadHandler(t, parser.RunReadRepository{DB: db})
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			method := "GET"
			if name == "POST remains unported" {
				method = "POST"
			}
			r := httptest.NewRequest(method, "https://example.invalid/api/scheduler/run?"+v.Query, strings.NewReader("{}")).WithContext(ctx)
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens["Eugene"]})
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handler.ServeHTTP(w, r); close(done) }()
			if hook.entered != nil {
				defer func() {
					select {
					case <-hook.release:
					default:
						close(hook.release)
					}
				}()
				awaitSettingsBarrier(t, hook.entered)
				if name == "cancelled run query" {
					cancel()
				} else {
					tx, e := f.db.BeginTx(f.ctx, nil)
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback()
					if _, e = tx.ExecContext(f.ctx, "UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1"); e != nil {
						t.Fatal(e)
					}
					if _, e = tx.ExecContext(f.ctx, "UPDATE scheduler_runs SET status='completed',finished_at=9999 WHERE id=?", v.Runs[0]["id"]); e != nil {
						t.Fatal(e)
					}
					if tx.Commit() != nil {
						t.Fatal("snapshot writer failed")
					}
					before = dashboardFingerprint(f)
				}
				close(hook.release)
			}
			select {
			case <-done:
			case <-f.ctx.Done():
				t.Fatal("run read timed out")
			}
			status := 500
			switch name {
			case "consistent snapshot":
				status = 200
				var got, want any
				if json.Unmarshal(w.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(v.ResponseBody), &want) != nil || !reflect.DeepEqual(got, want) || !hook.readOnly.Load() || hook.isolation.Load() != int64(sql.LevelRepeatableRead) {
					t.Fatal("run snapshot mixed state or writable")
				}
			case "generation before snapshot":
				status = 409
			case "POST remains unported":
				status = 503
			}
			if w.Code != status || before != dashboardFingerprint(f) || strings.Contains(w.Body.String(), errSettingsInjected.Error()) {
				t.Fatal("run fault/status/immutability/redaction differs")
			}
			if (name != "POST remains unported") != hook.fired.Load() {
				t.Fatal("run query fault/barrier did not fire or POST reached storage")
			}
		})
	}
}
