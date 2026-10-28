//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/parser"
)

func TestMariaDBSchedulerDashboardSnapshotDisposable(t *testing.T) {
	requireSettingsCI(t)
	const raw = `{"revision":7}`
	seed := func(f *settingsSQLFixture) {
		seedDashboard(f, dashboardSQLFrame{Raw: raw, Favorite: 1, Legacy: []string{"hh"}})
	}
	t.Run("отсутствующий singleton не создаётся", func(t *testing.T) {
		f := dashboardFixture(t)
		before := dashboardFingerprint(f)
		if _, e := (parser.DashboardRepository{DB: f.reader}).ReadDashboard(f.ctx); e != nil || f.row() != nil || dashboardFingerprint(f) != before {
			t.Fatal("GET записал отсутствующие настройки")
		}
	})
	t.Run("согласованный снимок при смене поколения", func(t *testing.T) {
		f := dashboardFixture(t)
		seed(f)
		hook := &settingsHook{query: "SELECT status,count(*)", entered: make(chan struct{}), release: make(chan struct{})}
		db := settingsFaultPool(f, hook, true)
		var once sync.Once
		release := func() { once.Do(func() { close(hook.release) }) }
		defer release()
		type result struct {
			body json.RawMessage
			err  error
		}
		done := make(chan result, 1)
		go func() { body, e := (parser.DashboardRepository{DB: db}).ReadDashboard(f.ctx); done <- result{body, e} }()
		awaitSettingsBarrier(t, hook.entered)
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		const next = "33333333-4444-4555-8666-777777777777"
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"UPDATE app_runtime_generation SET generation=? WHERE id=1", []any{next}},
			{`UPDATE firecrawl_settings SET scheduler_json='{"revision":8}' WHERE id=1`, nil},
			{"INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,priority,run_after,created_at,updated_at) VALUES('CI_new',?,'CI_new','diagnostic',0,1,1,1)", []any{next}},
		} {
			if _, e = tx.ExecContext(f.ctx, q.sql, q.args...); e != nil {
				t.Fatal("Не выполнена синтетическая смена поколения")
			}
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		release()
		got := <-done
		var old struct {
			Settings struct{ Revision int }
			Counts   map[string]int
		}
		if got.err != nil || json.Unmarshal(got.body, &old) != nil || old.Settings.Revision != 7 || len(old.Counts) != 0 || !hook.readOnly.Load() || hook.isolation.Load() != int64(sql.LevelRepeatableRead) {
			t.Fatal("GET смешал данные двух поколений")
		}
		fresh, e := (parser.DashboardRepository{DB: f.reader}).ReadDashboard(f.ctx)
		var current struct {
			Settings struct{ Revision int }
			Counts   map[string]int
		}
		if e != nil || json.Unmarshal(fresh, &current) != nil || current.Settings.Revision != 8 || current.Counts["pending"] != 1 {
			t.Fatal("Новый GET не увидел новое поколение")
		}
	})
	t.Run("поздний отказ SELECT не возвращает частичный DTO", func(t *testing.T) {
		f := dashboardFixture(t)
		seed(f)
		before := dashboardFingerprint(f)
		hook := &settingsHook{query: "SELECT a.id,a.job_id", failQuery: true}
		body, e := (parser.DashboardRepository{DB: settingsFaultPool(f, hook, true)}).ReadDashboard(f.ctx)
		if !errors.Is(e, parser.ErrDashboardStorage) || len(body) != 0 || !hook.fired.Load() || dashboardFingerprint(f) != before {
			t.Fatal("Поздний SQL отказ дал частичный ответ или изменил данные")
		}
	})
	t.Run("отмена между SELECT не меняет данные", func(t *testing.T) {
		f := dashboardFixture(t)
		seed(f)
		before := dashboardFingerprint(f)
		hook := &settingsHook{query: "SELECT status,count(*)", entered: make(chan struct{}), release: make(chan struct{})}
		db := settingsFaultPool(f, hook, true)
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		defer close(hook.release)
		done := make(chan error, 1)
		go func() { _, e := (parser.DashboardRepository{DB: db}).ReadDashboard(ctx); done <- e }()
		awaitSettingsBarrier(t, hook.entered)
		cancel()
		if e := <-done; !errors.Is(e, context.Canceled) || dashboardFingerprint(f) != before {
			t.Fatal("Отменённый GET изменил данные или скрыл отмену")
		}
	})
	t.Run("целое переполнение found отклоняется как в SQLite", func(t *testing.T) {
		f := dashboardFixture(t)
		seed(f)
		for _, v := range []struct{ id, raw string }{{"1", `{"found":9223372036854775807}`}, {"2", `{"found":1}`}} {
			f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,result_json) VALUES(?,'CI_job','CI_run','scan_source',1,'completed',?)", v.id, v.raw)
		}
		before := dashboardFingerprint(f)
		if body, e := (parser.DashboardRepository{DB: f.reader, Now: func() time.Time { return time.UnixMilli(1) }}).ReadDashboard(f.ctx); !errors.Is(e, parser.ErrDashboardStorage) || len(body) != 0 || dashboardFingerprint(f) != before {
			t.Fatal("SQLite integer overflow заменён успешным ответом")
		}
	})
}
