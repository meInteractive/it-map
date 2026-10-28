//go:build mariadb && schedulerwrites && schedulersettings

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/parser"
)

var errSettingsInjected = errors.New("CI_SETTINGS_FAULT_DETAILS_NOT_PUBLIC")

type settingsHook struct {
	pauseBegin, failAfterUpdate, unknownCommit bool
	query                                      string
	entered, release                           chan struct{}
	fired                                      atomic.Bool
	updates, commits, isolation                atomic.Int64
	readOnly                                   atomic.Bool
}

func (h *settingsHook) pause(ctx context.Context) error {
	if !h.fired.CompareAndSwap(false, true) {
		return nil
	}
	close(h.entered)
	select {
	case <-h.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type settingsConnector struct {
	driver.Connector
	hook *settingsHook
}

func (c settingsConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &settingsConn{Conn: conn, hook: c.hook}, nil
}

type settingsConn struct {
	driver.Conn
	hook *settingsHook
}

func (c *settingsConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.hook.readOnly.Store(opts.ReadOnly)
	c.hook.isolation.Store(int64(opts.Isolation))
	if c.hook.pauseBegin {
		if e := c.hook.pause(ctx); e != nil {
			return nil, e
		}
	}
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &settingsDriverTx{Tx: tx, hook: c.hook}, nil
}
func (c *settingsConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if c.hook.query != "" && strings.HasPrefix(q, c.hook.query) {
		if e := c.hook.pause(ctx); e != nil {
			return nil, e
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *settingsConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	result, e := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	if strings.HasPrefix(q, "UPDATE firecrawl_settings SET scheduler_json=") {
		c.hook.updates.Add(1)
		if e == nil && c.hook.failAfterUpdate && c.hook.fired.CompareAndSwap(false, true) {
			return result, errSettingsInjected
		}
	}
	return result, e
}

type settingsDriverTx struct {
	driver.Tx
	hook *settingsHook
}

func (tx *settingsDriverTx) Commit() error {
	tx.hook.commits.Add(1)
	e := tx.Tx.Commit()
	if e == nil && tx.hook.unknownCommit && tx.hook.fired.CompareAndSwap(false, true) {
		return errSettingsInjected
	}
	return e
}
func settingsFaultPool(f *settingsSQLFixture, hook *settingsHook, reader bool) *sql.DB {
	f.t.Helper()
	key := "ITMAP_TEST_MARIADB_WRITE_DSN"
	user := "itmap_ci"
	if reader {
		key = "ITMAP_TEST_MARIADB_DSN"
		user = "itmap_readonly_ci"
	}
	cfg, e := mysql.ParseDSN(os.Getenv(key))
	if e != nil || cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:3306" || cfg.DBName != "itmap_ci" || cfg.User != user {
		f.t.Fatal("refusing unsafe settings interception target")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("cannot create guarded settings connector")
	}
	db := sql.OpenDB(settingsConnector{Connector: connector, hook: hook})
	f.t.Cleanup(func() { db.Close() })
	return db
}
func awaitSettingsBarrier(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual settings operation did not reach required barrier")
	}
}

func TestMariaDBSchedulerSettingsFaultsDisposable(t *testing.T) {
	requireSettingsCI(t)
	v := settingsCases(t)[0]
	t.Run("late SQL result failure rolls back fresh pool", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(v)
		before := f.row()
		hook := &settingsHook{failAfterUpdate: true}
		db := settingsFaultPool(f, hook, false)
		_, e := (parser.SettingsRepository{DB: db}).Save(f.ctx, v.Input, "Eugene")
		if !errors.Is(e, parser.ErrSettingsStorage) || hook.updates.Load() != 1 || !hook.fired.Load() || len(settingsRowDifferences(before, f.row())) != 0 {
			t.Fatal("late settings fault escaped rollback")
		}
		fresh, e := (parser.SettingsRepository{DB: f.reader}).Read(f.ctx)
		if e != nil || strings.Contains(string(fresh), `"revision":8`) {
			t.Fatal("fresh reader observed rolled-back settings")
		}
	})
	t.Run("committed but unknown result is not retried", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(v)
		hook := &settingsHook{unknownCommit: true}
		db := settingsFaultPool(f, hook, false)
		repo := parser.SettingsRepository{DB: db}
		if _, e := repo.Save(f.ctx, v.Input, "Eugene"); !errors.Is(e, parser.ErrSettingsStorage) || hook.updates.Load() != 1 || hook.commits.Load() != 1 {
			t.Fatal("unknown COMMIT was retried or reported successful")
		}
		fresh, e := (parser.SettingsRepository{DB: f.reader}).Read(f.ctx)
		if e != nil || string(fresh) != v.Body {
			t.Fatal("known committed test outcome not visible to fresh pool")
		}
		if _, e = repo.Save(f.ctx, v.Input, "Eugene"); !errors.Is(e, parser.ErrSettingsChanged) || hook.updates.Load() != 1 {
			t.Fatal("original CAS replay overwrote acknowledged committed state")
		}
	})
	t.Run("generation rotates after capture before lock", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(v)
		before := f.row()
		hook := &settingsHook{pauseBegin: true, entered: make(chan struct{}), release: make(chan struct{})}
		db := settingsFaultPool(f, hook, false)
		var once sync.Once
		release := func() { once.Do(func() { close(hook.release) }) }
		defer release()
		done := make(chan error, 1)
		go func() { _, e := (parser.SettingsRepository{DB: db}).Save(f.ctx, v.Input, "Eugene"); done <- e }()
		awaitSettingsBarrier(t, hook.entered)
		f.exec("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1")
		release()
		if e := <-done; !errors.Is(e, parser.ErrSettingsGeneration) || hook.updates.Load() != 0 || len(settingsRowDifferences(before, f.row())) != 0 {
			t.Fatal("old captured generation wrote after rotation")
		}
	})
	t.Run("readonly snapshot does not mix source scope", func(t *testing.T) {
		f := openSettingsSQL(t)
		f.seed(v)
		hook := &settingsHook{query: "SELECT DISTINCT source_id", entered: make(chan struct{}), release: make(chan struct{})}
		db := settingsFaultPool(f, hook, true)
		var once sync.Once
		release := func() { once.Do(func() { close(hook.release) }) }
		defer release()
		type result struct {
			body json.RawMessage
			err  error
		}
		done := make(chan result, 1)
		go func() { body, e := (parser.SettingsRepository{DB: db}).Read(f.ctx); done <- result{body, e} }()
		awaitSettingsBarrier(t, hook.entered)
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(f.ctx, `UPDATE firecrawl_settings SET scheduler_json='{"revision":8}' WHERE id=1`); e != nil {
			t.Fatal("synthetic snapshot writer failed")
		}
		if _, e = tx.ExecContext(f.ctx, "UPDATE vacancy_source_parsing SET auto_enabled=0"); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(f.ctx, "INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,auto_enabled) VALUES(1,'habr','https://example.invalid/habr',1)"); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		release()
		got := <-done
		if got.err != nil || !strings.Contains(string(got.body), `"revision":7`) || !strings.Contains(string(got.body), `"sourceIds":["hh"]`) {
			t.Fatal("settings mixed old document and new source scope")
		}
		if !hook.readOnly.Load() || hook.isolation.Load() != int64(sql.LevelRepeatableRead) {
			t.Fatal("settings read did not use actual readonly repeatable snapshot")
		}
		fresh, e := (parser.SettingsRepository{DB: f.reader}).Read(f.ctx)
		if e != nil || !strings.Contains(string(fresh), `"revision":8`) || !strings.Contains(string(fresh), `"sourceIds":["habr"]`) {
			t.Fatal("fresh settings read did not observe committed source scope")
		}
	})
}
