//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"crypto/sha256"
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

type dashboardSQLFrame struct {
	Name, Raw            string
	Now, Favorite        int64
	Legacy               []string
	Jobs, Runs, Attempts []map[string]any
	Requests             []struct {
		User, Body string
		Status     int
	}
}

// ORDER BY started_at DESC не задаёт порядок внутри одинакового timestamp.
// Сравниваем полные записи каждой такой группы; между группами порядок строгий.
func comparableDashboardHistory(t *testing.T, value any) {
	t.Helper()
	root, ok := value.(map[string]any)
	if !ok {
		t.Fatal("Ответ dashboard не является объектом")
	}
	for _, key := range []string{"runs", "attempts"} {
		rows, ok := root[key].([]any)
		if !ok {
			continue // Ответ об отказе в доступе не содержит историю.
		}
		for start := 0; start < len(rows); {
			row := rows[start].(map[string]any)
			at := row["started_at"].(float64)
			if start > 0 && rows[start-1].(map[string]any)["started_at"].(float64) < at {
				t.Fatal("История не отсортирована по убыванию времени")
			}
			end := start + 1
			for end < len(rows) && rows[end].(map[string]any)["started_at"].(float64) == at {
				end++
			}
			group := rows[start:end]
			sort.Slice(group, func(i, j int) bool {
				return group[i].(map[string]any)["id"].(string) < group[j].(map[string]any)["id"].(string)
			})
			start = end
		}
	}
}

func dashboardFixture(t *testing.T) *settingsSQLFixture {
	t.Helper()
	f := openSettingsSQL(t)
	for _, table := range []string{"scheduler_attempts", "scheduler_jobs", "scheduler_runs"} {
		var n int
		if e := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("dashboard scheduler namespace must start empty")
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range []string{"scheduler_attempts", "scheduler_jobs", "scheduler_runs"} {
			if _, e := f.db.ExecContext(ctx, "DELETE FROM "+table); e != nil {
				t.Error("dashboard fixture cleanup failed")
			}
		}
	})
	return f
}
func dashboardFingerprint(f *settingsSQLFixture, ignoredTables ...string) [32]byte {
	f.t.Helper()
	var baseline schemaBaseline
	if json.Unmarshal(baselineJSON, &baseline) != nil || len(baseline.Tables) != 72 {
		f.t.Fatal("trusted dashboard baseline missing")
	}
	content := []string{}
	for table := range baseline.Tables {
		ignored := false
		for _, name := range ignoredTables {
			if table == name {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}
		rows, e := f.reader.QueryContext(f.ctx, "SELECT * FROM `"+table+"`")
		if e != nil {
			f.t.Fatal("dashboard snapshot query failed")
		}
		columns, e := rows.Columns()
		if e != nil {
			rows.Close()
			f.t.Fatal(e)
		}
		for rows.Next() {
			values, dest := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if e = rows.Scan(dest...); e != nil {
				rows.Close()
				f.t.Fatal("dashboard snapshot scan failed")
			}
			raw, e := json.Marshal(values)
			if e != nil {
				rows.Close()
				f.t.Fatal("dashboard snapshot encode failed")
			}
			content = append(content, table+":"+string(raw))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			f.t.Fatal("dashboard snapshot read failed")
		}
	}
	sort.Strings(content)
	return sha256.Sum256([]byte(strings.Join(content, "\n")))
}
func seedDashboard(f *settingsSQLFixture, v dashboardSQLFrame) {
	f.t.Helper()
	for _, table := range []string{"scheduler_attempts", "scheduler_jobs", "scheduler_runs"} {
		f.exec("DELETE FROM " + table)
	}
	f.seed(settingsSQLCase{Raw: &v.Raw, Favorite: v.Favorite, Legacy: v.Legacy})
	var baseline schemaBaseline
	if json.Unmarshal(baselineJSON, &baseline) != nil {
		f.t.Fatal("trusted dashboard baseline missing")
	}
	for _, set := range []struct {
		table string
		rows  []map[string]any
	}{{"scheduler_jobs", v.Jobs}, {"scheduler_runs", v.Runs}, {"scheduler_attempts", v.Attempts}} {
		for _, row := range set.rows {
			columns := []string{}
			for key := range row {
				if _, ok := baseline.Tables[set.table][key]; !ok {
					f.t.Fatal("unreviewed dashboard fixture column")
				}
				columns = append(columns, key)
			}
			sort.Strings(columns)
			quoted, marks, values := []string{}, []string{}, []any{}
			for _, key := range columns {
				quoted = append(quoted, "`"+key+"`")
				marks = append(marks, "?")
				values = append(values, row[key])
			}
			f.exec("INSERT INTO "+set.table+"("+strings.Join(quoted, ",")+") VALUES("+strings.Join(marks, ",")+")", values...)
		}
	}
}
func TestMariaDBSchedulerDashboardSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	m, e := auth.NewManager("CI_SYNTHETIC_DASHBOARD_SECRET_326", map[string]string{"eugene": "CI_pw", "demo": "CI_pw", "nastya": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "demo", "nastya"} {
		u, _ := m.Authenticate(login, "CI_pw")
		tokens[u.Name], e = m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command("node", "../../scripts/check-scheduler-dashboard-sql.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count, responses := json.NewDecoder(pipe), 0, 0
	for {
		var v dashboardSQLFrame
		e = decoder.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal("required source dashboard frame invalid")
		}
		t.Run(v.Name, func(t *testing.T) {
			seedDashboard(f, v)
			before := dashboardFingerprint(f)
			repo := parser.DashboardRepository{DB: f.reader, Now: func() time.Time { return time.UnixMilli(v.Now) }}
			handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerDashboard: repo})
			for _, c := range v.Requests {
				r := httptest.NewRequest("GET", "https://example.invalid/api/scheduler", nil)
				if c.User != "" {
					r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[c.User]})
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				var actual, expected any
				if w.Code != c.Status || json.Unmarshal(w.Body.Bytes(), &actual) != nil || json.Unmarshal([]byte(c.Body), &expected) != nil {
					t.Fatalf("source dashboard HTTP differs for %s: %d/%d", c.User, w.Code, c.Status)
				}
				comparableDashboardHistory(t, actual)
				comparableDashboardHistory(t, expected)
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("Поля dashboard отличаются от исходного ответа: %v", settingsRowDifferences(actual.(map[string]any), expected.(map[string]any)))
				}
				if strings.Contains(w.Body.String(), "CI_PRIVATE") || strings.Contains(w.Body.String(), "result_json") {
					t.Fatal("dashboard exposed private result/owner")
				}
				responses++
			}
			if dashboardFingerprint(f) != before {
				t.Fatal("dashboard GET changed one of72 table contents")
			}
		})
		count++
	}
	if e = cmd.Wait(); e != nil || count != 36 || responses != 144 {
		t.Fatal("required actual dashboard corpus incomplete", count, responses)
	}
	t.Log("PASS:36 source SQLite frames/144 actual Go→SELECT-only MariaDB HTTP responses; all72 table contents unchanged")
}
