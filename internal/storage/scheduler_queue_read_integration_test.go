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
	"sort"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/parser"
)

type queueReadFrame struct {
	Name, Query, User, Method, Body, Origin, Generation, ResponseBody string
	Status                                                            int
	NoDB                                                              bool
	Companies, Agencies, Candidates, Jobs                             []map[string]any
	QueryObject                                                       json.RawMessage
}

func queueReadFrames(t *testing.T) []queueReadFrame {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-scheduler-queue-read.mjs")
	cmd.Stderr = os.Stderr
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source queue oracle failed")
	}
	d, out := json.NewDecoder(bytes.NewReader(raw)), []queueReadFrame{}
	for {
		var v queueReadFrame
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, v)
	}
	if len(out) != 104 {
		t.Fatal("missing source queue frames", len(out))
	}
	return out
}
func queueReadFixture(t *testing.T) *settingsSQLFixture {
	t.Helper()
	f := dashboardFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	for _, table := range []string{"vacancy_candidates", "agency_identity"} {
		var n int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&n) != nil || n != 0 {
			t.Fatal("queue namespace must start empty")
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range []string{"scheduler_jobs", "vacancy_candidates", "agency_identity"} {
			if _, e := f.db.ExecContext(ctx, "DELETE FROM "+table); e != nil {
				t.Error("queue fixture cleanup failed")
			}
		}
	})
	return f
}
func seedQueueRead(f *settingsSQLFixture, v queueReadFrame) {
	f.exec("DELETE FROM scheduler_jobs")
	f.exec("DELETE FROM vacancy_candidates")
	f.exec("DELETE FROM agency_identity")
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.generation)
	seedDashboard(f, dashboardSQLFrame{Raw: `{"revision":7}`, Favorite: 1})
	var baseline schemaBaseline
	if json.Unmarshal(baselineJSON, &baseline) != nil {
		f.t.Fatal("missing trusted baseline")
	}
	for _, set := range []struct {
		table string
		rows  []map[string]any
	}{{"company_identity", v.Companies}, {"agency_identity", v.Agencies}, {"vacancy_candidates", v.Candidates}, {"scheduler_jobs", v.Jobs}} {
		for _, row := range set.rows {
			if set.table == "company_identity" && row["id"] == float64(1) {
				f.exec("UPDATE company_identity SET company_name=? WHERE id=1", row["company_name"])
				continue
			}
			keys := []string{}
			for key := range row {
				if _, ok := baseline.Tables[set.table][key]; !ok {
					f.t.Fatal("unreviewed fixture column")
				}
				keys = append(keys, key)
			}
			sort.Strings(keys)
			columns, marks, args := []string{}, []string{}, []any{}
			for _, key := range keys {
				value := row[key]
				if key == "generation" && value == v.Generation {
					value = f.generation
				}
				columns = append(columns, "`"+key+"`")
				marks = append(marks, "?")
				args = append(args, value)
			}
			f.exec("INSERT INTO "+set.table+"("+strings.Join(columns, ",")+") VALUES("+strings.Join(marks, ",")+")", args...)
		}
	}
	f.exec("UPDATE scheduler_jobs SET priority=9007199254740993 WHERE id='00000000-0000-4000-8000-000000000001'")
}
func queueReadHandler(t *testing.T, store httpapi.SchedulerQueueReader) (http.Handler, map[string]string) {
	t.Helper()
	m, e := auth.NewManager("CI_SYNTHETIC_QUEUE_READ_SECRET_330", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
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
	return httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerQueue: store}), tokens
}
func normalizeQueueGeneration(t *testing.T, raw []byte, from, to string) any {
	t.Helper()
	var out any
	if json.Unmarshal(raw, &out) != nil {
		t.Fatal("invalid queue JSON")
	}
	if root, ok := out.(map[string]any); ok {
		if jobs, ok := root["jobs"].([]any); ok {
			for _, job := range jobs {
				row := job.(map[string]any)
				if row["generation"] == from {
					row["generation"] = to
				}
				if _, ok := row["payload"]; ok {
					t.Fatal("queue exposes payload")
				}
				if _, ok := row["_d1_active_scan"]; ok {
					t.Fatal("queue exposes native service column")
				}
			}
		}
	}
	return out
}
func TestMariaDBSchedulerQueueSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := queueReadFixture(t)
	handler, tokens := queueReadHandler(t, parser.QueueRepository{DB: f.reader})
	noDB, _ := queueReadHandler(t, nil)
	for _, v := range queueReadFrames(t) {
		t.Run(v.Name, func(t *testing.T) {
			seedQueueRead(f, v)
			before := dashboardFingerprint(f)
			uri := "https://example.invalid/api/scheduler/queue"
			if v.Query != "" {
				uri += "?" + v.Query
			}
			r := httptest.NewRequest(v.Method, uri, strings.NewReader(v.Body))
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
			got := normalizeQueueGeneration(t, w.Body.Bytes(), f.generation, v.Generation)
			want := normalizeQueueGeneration(t, []byte(v.ResponseBody), v.Generation, v.Generation)
			if w.Code != v.Status || !reflect.DeepEqual(got, want) {
				t.Fatalf("source queue HTTP/DTO differs: %d/%d", w.Code, v.Status)
			}
			if strings.Contains(w.Body.String(), "CI_QUEUE_PAYLOAD_MUST_BE_REMOVED") || before != dashboardFingerprint(f) {
				t.Fatal("queue leaked payload or changed one of 72 tables")
			}
		})
	}
	t.Log("PASS: 104 full source router/SQLite queue responses vs Go/MariaDB, readonly account and 72-table immutability")
}
func TestMariaDBSchedulerQueueSnapshotDisposable(t *testing.T) {
	requireSettingsCI(t)
	for _, name := range []string{"consistent snapshot", "late page query fault", "cancelled count query", "capture failure before invalid query"} {
		t.Run(name, func(t *testing.T) {
			f := queueReadFixture(t)
			v := queueReadFrames(t)[0]
			seedQueueRead(f, v)
			before := dashboardFingerprint(f)
			hook := &settingsHook{}
			switch name {
			case "late page query fault":
				hook.query = "SELECT j.id,j.generation"
				hook.failQuery = true
			case "capture failure before invalid query":
				hook.query = "SELECT generation FROM app_runtime_generation WHERE id=1"
				hook.failQuery = true
			default:
				hook.query = "SELECT COUNT(*) AS total FROM scheduler_jobs"
				hook.entered = make(chan struct{})
				hook.release = make(chan struct{})
			}
			db := settingsFaultPool(f, hook, true)
			repo := parser.QueueRepository{DB: db}
			handler, tokens := queueReadHandler(t, repo)
			uri := "https://example.invalid/api/scheduler/queue"
			if name == "capture failure before invalid query" {
				uri += "?page=0"
			}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			r := httptest.NewRequest("GET", uri, nil).WithContext(ctx)
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
				if name == "consistent snapshot" {
					tx, e := f.db.BeginTx(f.ctx, nil)
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback()
					if _, e = tx.ExecContext(f.ctx, "UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1"); e != nil {
						t.Fatal(e)
					}
					if _, e = tx.ExecContext(f.ctx, "UPDATE scheduler_jobs SET status='cancelled' WHERE generation=?", f.generation); e != nil {
						t.Fatal(e)
					}
					if _, e = tx.ExecContext(f.ctx, "UPDATE company_identity SET company_name='CI NEW SNAPSHOT' WHERE id=1"); e != nil {
						t.Fatal(e)
					}
					if _, e = tx.ExecContext(f.ctx, "UPDATE vacancy_candidates SET title_raw='CI NEW TITLE' WHERE id=1"); e != nil {
						t.Fatal(e)
					}
					if tx.Commit() != nil {
						t.Fatal("snapshot writer failed")
					}
					before = dashboardFingerprint(f)
				} else {
					cancel()
				}
				close(hook.release)
			}
			select {
			case <-done:
			case <-f.ctx.Done():
				t.Fatal("queue read did not finish")
			}
			if name == "consistent snapshot" {
				got := normalizeQueueGeneration(t, w.Body.Bytes(), f.generation, v.Generation)
				want := normalizeQueueGeneration(t, []byte(v.ResponseBody), v.Generation, v.Generation)
				if w.Code != 200 || !reflect.DeepEqual(got, want) || !hook.readOnly.Load() || hook.isolation.Load() != int64(sql.LevelRepeatableRead) {
					t.Fatal("queue mixed generations/count/page/names/title or used writable snapshot")
				}
			} else if w.Code != 500 || strings.Contains(w.Body.String(), errSettingsInjected.Error()) {
				t.Fatal("failed queue read returned partial success/private error")
			}
			if !hook.fired.Load() || before != dashboardFingerprint(f) {
				t.Fatal("queue fault/snapshot probe did not fire or wrote data")
			}
		})
	}
}
