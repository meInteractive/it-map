//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
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

// Смена поколения происходит синхронно при первом Read, после capture и до SQL write.
type settingsHTTPBody struct {
	io.Reader
	before func()
	reads  int
}

func (b *settingsHTTPBody) Read(p []byte) (int, error) {
	if b.reads == 0 {
		b.before()
	}
	b.reads++
	return b.Reader.Read(p)
}
func (b *settingsHTTPBody) Close() error { return nil }

func settingsBodyHandler(t *testing.T, repo parser.SettingsRepository) (http.Handler, string) {
	t.Helper()
	m, e := auth.NewManager("CI_SYNTHETIC_SCHEDULER_HTTP_SECRET_324", map[string]string{"eugene": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(u, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, SchedulerSettings: repo}), token
}

func TestMariaDBSchedulerSettingsBodySourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	// Единственное соединение доказывает, что capture не держит его до конца body.
	f.db.SetMaxOpenConns(1)
	handler, token := settingsBodyHandler(t, parser.SettingsRepository{DB: f.db})
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-scheduler-settings-body.mjs")
	cmd.Stderr = os.Stderr
	frames, e := cmd.Output()
	if e != nil {
		t.Fatal("source body oracle не выполнен")
	}
	decoder, count := json.NewDecoder(bytes.NewReader(frames)), 0
	for {
		var v struct {
			Name, Method, Action, RestoredRaw, Body string
			Fixture                                 settingsSQLCase
			RequestBody                             []byte
			Status                                  int
			Row                                     map[string]any
		}
		if e = decoder.Decode(&v); e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal("ошибка декодирования source frame")
		}
		t.Run(v.Name, func(t *testing.T) {
			f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.generation)
			f.seed(v.Fixture)
			var before [32]byte
			body := &settingsHTTPBody{Reader: bytes.NewReader(v.RequestBody), before: func() {
				tx, e := f.db.BeginTx(f.ctx, nil)
				if e != nil {
					t.Fatal("не началась synthetic rotation")
				}
				defer tx.Rollback()
				if v.Action != "none" {
					if _, e = tx.ExecContext(f.ctx, "UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1"); e != nil {
						t.Fatal(e)
					}
				}
				if v.Action == "replace" {
					_, e = tx.ExecContext(f.ctx, "UPDATE firecrawl_settings SET scheduler_json=?,favorites_only=1,revision=41 WHERE id=1", v.RestoredRaw)
				}
				if v.Action == "delete" {
					_, e = tx.ExecContext(f.ctx, "DELETE FROM firecrawl_settings WHERE id=1")
				}
				if e != nil {
					t.Fatal("не выполнена synthetic restore state replacement")
				}
				if e = tx.Commit(); e != nil {
					t.Fatal(e)
				}
				before = dashboardFingerprint(f)
			}}
			r := httptest.NewRequest(v.Method, "https://example.invalid/api/scheduler/settings", nil)
			r.Body = body
			r.ContentLength = -1
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var got, want any
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(v.Body), &want) != nil || w.Code != v.Status || !reflect.DeepEqual(got, want) {
				t.Fatalf("source status/JSON отличается: %d/%d", w.Code, v.Status)
			}
			if body.reads == 0 {
				t.Fatal("source frame не достиг body barrier")
			}
			if diff := settingsRowDifferences(f.row(), v.Row); len(diff) != 0 {
				t.Fatalf("отличаются поля settings: %v", diff)
			}
			if v.Status != 200 && before != dashboardFingerprint(f) {
				t.Fatal("отклонённый HTTP запрос изменил одну из 72 таблиц")
			}
			expectedGeneration := f.generation
			if v.Action != "none" {
				expectedGeneration = "22222222-3333-4333-8444-555555555555"
			}
			var generation string
			if e = f.reader.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || generation != expectedGeneration {
				t.Fatal("HTTP изменил поколение фикстуры")
			}
		})
		count++
	}
	if count != 8 {
		t.Fatal("неполный source body-generation corpus", count)
	}
	t.Log("PASS: 8 unchanged source router/SQLite vs Go/MariaDB body-generation responses; one-connection pool; full row and rejected 72-table immutability")
}

func TestMariaDBSchedulerSettingsCaptureDisposable(t *testing.T) {
	requireSettingsCI(t)
	for _, name := range []string{"capture query failure before oversized body", "cancelled capture"} {
		t.Run(name, func(t *testing.T) {
			f := openSettingsSQL(t)
			f.seed(settingsCases(t)[0])
			before := dashboardFingerprint(f)
			hook := &settingsHook{query: "SELECT generation FROM app_runtime_generation WHERE id=1", failQuery: true}
			db := f.db
			if name != "cancelled capture" {
				db = settingsFaultPool(f, hook, false)
			}
			handler, token := settingsBodyHandler(t, parser.SettingsRepository{DB: db})
			body := &settingsHTTPBody{Reader: strings.NewReader(`{"revision":7}`), before: func() { t.Error("capture failure прочитал body") }}
			r := httptest.NewRequest("PUT", "https://example.invalid/api/scheduler/settings", nil)
			r.Body = body
			r.ContentLength = 32769
			if name == "cancelled capture" {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 500 || body.reads != 0 || before != dashboardFingerprint(f) || strings.Contains(w.Body.String(), errSettingsInjected.Error()) {
				t.Fatal("capture precedence/immutability/redaction нарушены")
			}
			if name != "cancelled capture" && !hook.fired.Load() {
				t.Fatal("SQL fault не сработал")
			}
		})
	}
}
