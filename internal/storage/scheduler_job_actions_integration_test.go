//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

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

type jobActionFrame struct {
	Name, Action, State, User, Body, Method, Origin, ContentLength, URLId, URLAction, Query, Generation, ID string
	Now                                                                                                     int64
	NoDB, Old, Missing, Rotate                                                                              bool
	Repeat                                                                                                  int
	Jobs, Attempts, FinalJobs, FinalAttempts                                                                []map[string]any
	RequestBody                                                                                             []byte
	Responses                                                                                               []struct {
		Status int
		Body   string
	}
}

func jobActionFrames(t *testing.T) []jobActionFrame {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-scheduler-job-actions.mjs")
	cmd.Stderr = os.Stderr
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source job action oracle не выполнен")
	}
	d, out := json.NewDecoder(bytes.NewReader(raw)), []jobActionFrame{}
	for {
		var v jobActionFrame
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, v)
	}
	if len(out) != 43 {
		t.Fatal("неполный job action corpus", len(out))
	}
	return out
}
func jobActionHandler(t *testing.T, store httpapi.SchedulerJobActionWriter) (http.Handler, map[string]string) {
	t.Helper()
	m, e := auth.NewManager("CI_SYNTHETIC_SCHEDULER_ACTION_SECRET_329", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, name := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(name, "CI_pw")
		token, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		tokens[u.Name] = token
	}
	return httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerJobActions: store}), tokens
}
func actionRows(t *testing.T, f *settingsSQLFixture, table string) []map[string]any {
	t.Helper()
	if table != "scheduler_jobs" && table != "scheduler_attempts" {
		t.Fatal("неразрешённая fixture table")
	}
	rows, e := f.reader.QueryContext(f.ctx, "SELECT * FROM "+table+" ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	columns, e := rows.Columns()
	if e != nil {
		t.Fatal(e)
	}
	out := []map[string]any{}
	for rows.Next() {
		values, dest := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if rows.Scan(dest...) != nil {
			t.Fatal("fixture scan failed")
		}
		row := map[string]any{}
		for i, key := range columns {
			if b, ok := values[i].([]byte); ok {
				row[key] = string(b)
			} else {
				row[key] = values[i]
			}
		}
		raw, e := json.Marshal(row)
		if e != nil {
			t.Fatal(e)
		}
		if json.Unmarshal(raw, &row) != nil {
			t.Fatal("fixture normalization failed")
		}
		out = append(out, row)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return out
}
func seedJobAction(f *settingsSQLFixture, v jobActionFrame) {
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.generation)
	for _, row := range v.Jobs {
		if row["generation"] == v.Generation {
			row["generation"] = f.generation
		}
	}
	seedDashboard(f, dashboardSQLFrame{Raw: `{"revision":7}`, Favorite: 1, Jobs: v.Jobs, Attempts: v.Attempts})
}
func jobActionRequest(v jobActionFrame, token string, body io.ReadCloser) *http.Request {
	id := v.ID
	if v.URLId != "" {
		id = v.URLId
	}
	action := v.Action
	if v.URLAction != "" {
		action = v.URLAction
	}
	r := httptest.NewRequest(v.Method, "https://example.invalid/api/scheduler/jobs/"+id+"/"+action+v.Query, nil)
	if v.Method != "GET" {
		r.Body = body
		r.ContentLength = -1
	}
	if v.ContentLength != "" {
		r.ContentLength = 32769
	}
	r.Header.Set("Origin", v.Origin)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	}
	return r
}
func normalizeActionGeneration(rows []map[string]any, from, to string) {
	for _, row := range rows {
		if row["generation"] == from {
			row["generation"] = to
		}
	}
}

func TestMariaDBSchedulerJobActionsSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	f.db.SetMaxOpenConns(1)
	f.exec("SET timestamp=1800000000")
	t.Cleanup(func() { f.exec("SET timestamp=0") })
	handler, tokens := jobActionHandler(t, parser.JobActionsRepository{DB: f.db})
	noDB, _ := jobActionHandler(t, nil)
	count := 0
	for _, v := range jobActionFrames(t) {
		t.Run(v.Name, func(t *testing.T) {
			seedJobAction(f, v)
			before := dashboardFingerprint(f, "scheduler_jobs")
			for _, expected := range v.Responses {
				body := &settingsHTTPBody{Reader: bytes.NewReader(v.RequestBody), before: func() {
					if v.Rotate {
						f.exec("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1")
						before = dashboardFingerprint(f, "scheduler_jobs")
					}
				}}
				r := jobActionRequest(v, tokens[v.User], body)
				w := httptest.NewRecorder()
				h := handler
				if v.NoDB {
					h = noDB
				}
				h.ServeHTTP(w, r)
				var got, want any
				if json.Unmarshal(w.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(expected.Body), &want) != nil || w.Code != expected.Status || !reflect.DeepEqual(got, want) {
					t.Fatalf("source HTTP/JSON отличается: %d/%d", w.Code, expected.Status)
				}
			}
			jobs := actionRows(t, f, "scheduler_jobs")
			normalizeActionGeneration(jobs, f.generation, v.Generation)
			if !reflect.DeepEqual(jobs, v.FinalJobs) {
				t.Fatal("отличаются полные строки scheduler_jobs")
			}
			if !reflect.DeepEqual(actionRows(t, f, "scheduler_attempts"), v.FinalAttempts) {
				t.Fatal("команда изменила финансовый журнал")
			}
			if before != dashboardFingerprint(f, "scheduler_jobs") {
				t.Fatal("команда изменила другую таблицу")
			}
		})
		count++
	}
	if count != 43 {
		t.Fatal("пропущены source frames")
	}
	t.Log("PASS: 43 source router/SQLite frames, 45 HTTP responses, full jobs/ledger and 71-table immutability")
}

func TestMariaDBSchedulerJobActionsFaultsDisposable(t *testing.T) {
	requireSettingsCI(t)
	for _, name := range []string{"late UPDATE fault rollback", "unknown committed outcome", "restore before lock", "job becomes running before lock"} {
		t.Run(name, func(t *testing.T) {
			f := dashboardFixture(t)
			v := jobActionFrames(t)[0]
			seedJobAction(f, v)
			hook := &settingsHook{updatePrefix: "UPDATE scheduler_jobs SET status="}
			switch name {
			case "late UPDATE fault rollback":
				hook.failAfterUpdate = true
			case "unknown committed outcome":
				hook.unknownCommit = true
			default:
				hook.pauseBegin = true
				hook.entered = make(chan struct{})
				hook.release = make(chan struct{})
			}
			db := settingsFaultPool(f, hook, false)
			handler, tokens := jobActionHandler(t, parser.JobActionsRepository{DB: db})
			before := dashboardFingerprint(f)
			w := httptest.NewRecorder()
			r := jobActionRequest(v, tokens[v.User], io.NopCloser(strings.NewReader("{}")))
			done := make(chan struct{})
			go func() { handler.ServeHTTP(w, r); close(done) }()
			if hook.pauseBegin {
				defer func() {
					select {
					case <-hook.release:
					default:
						close(hook.release)
					}
				}()
				awaitSettingsBarrier(t, hook.entered)
				if name == "restore before lock" {
					f.exec("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1")
				} else {
					f.exec("UPDATE scheduler_jobs SET status='running' WHERE id=?", v.ID)
				}
				before = dashboardFingerprint(f)
				close(hook.release)
			}
			select {
			case <-done:
			case <-f.ctx.Done():
				t.Fatal("job action не завершилась")
			}
			if strings.Contains(w.Body.String(), errSettingsInjected.Error()) {
				t.Fatal("SQL fault попал в ответ")
			}
			if name == "unknown committed outcome" {
				if w.Code != 500 || hook.updates.Load() != 1 || hook.commits.Load() != 1 {
					t.Fatal("unknown COMMIT был повторён")
				}
				rows := actionRows(t, f, "scheduler_jobs")
				var updated bool
				for _, row := range rows {
					if row["id"] == v.ID {
						updated = row["attempts"] == float64(0) && row["last_error"] == ""
					}
				}
				if !updated {
					t.Fatal("подтверждённый тестовый COMMIT не виден свежему reader")
				}
			} else {
				status := 500
				if hook.pauseBegin {
					status = 409
				}
				if w.Code != status || before != dashboardFingerprint(f) {
					t.Fatal("failed/racing command изменила данные или статус")
				}
			}
			if name == "late UPDATE fault rollback" && (!hook.fired.Load() || hook.updates.Load() != 1 || hook.commits.Load() != 0) {
				t.Fatal("UPDATE fault не доказал rollback")
			}
		})
	}
}
