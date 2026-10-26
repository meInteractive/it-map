//go:build mariadb && profilewrites && v39exports

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/backup"
)

func exportReadOnlyPool(f *profileSettingsFixture) *sql.DB {
	f.t.Helper()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_DSN"))
	if e != nil || cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:3306" || cfg.DBName != "itmap_ci" || cfg.User != "itmap_readonly_ci" {
		f.t.Fatal("required disposable read-only export DSN invalid")
	}
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		f.t.Fatal("cannot open disposable export reader")
	}
	f.t.Cleanup(func() { db.Close() })
	var generation string
	if e = db.QueryRowContext(f.ctx, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&generation); e != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
		f.t.Fatal("export reader generation mismatch")
	}
	return db
}
func exportPayload(t *testing.T, file backup.Download) *backup.Payload {
	t.Helper()
	p, e := backup.ValidateV39(file.Bytes)
	if e != nil || len(file.Bytes) == 0 || file.CreatedAt.IsZero() {
		t.Fatal("invalid synthetic SQL export")
	}
	return p
}
func exportTable(t *testing.T, p *backup.Payload, name string) backup.Table {
	t.Helper()
	for _, table := range p.Storage.Tables {
		if table.Name == name {
			return table
		}
	}
	t.Fatal("missing export section")
	return backup.Table{}
}

var errExportQuery = errors.New("CI_EXPORT_SQL_DETAILS_NOT_PUBLIC")

type exportIntercept struct {
	mu               sync.Mutex
	prefix           string
	entered, release chan struct{}
	fail, fired      bool
	readOnly         bool
}
type exportConnector struct {
	driver.Connector
	hook *exportIntercept
}

func (c exportConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &exportConn{Conn: conn, hook: c.hook}, nil
}

type exportConn struct {
	driver.Conn
	hook *exportIntercept
}

func (c *exportConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.hook.mu.Lock()
	c.hook.readOnly = opts.ReadOnly
	c.hook.mu.Unlock()
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *exportConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, c.hook.prefix) {
		c.hook.mu.Lock()
		first := !c.hook.fired
		if first {
			c.hook.fired = true
		}
		c.hook.mu.Unlock()
		if first {
			if c.hook.entered != nil {
				close(c.hook.entered)
				select {
				case <-c.hook.release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if c.hook.fail {
				return nil, errExportQuery
			}
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func interceptedExportPool(f *profileSettingsFixture, hook *exportIntercept) *sql.DB {
	f.t.Helper()
	guard := exportReadOnlyPool(f)
	_ = guard.Close()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_DSN"))
	if e != nil {
		f.t.Fatal("cannot configure export interception")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("cannot initialize export reader")
	}
	db := sql.OpenDB(exportConnector{Connector: connector, hook: hook})
	db.SetMaxOpenConns(1)
	f.t.Cleanup(func() { db.Close() })
	return db
}
func waitExportBarrier(t *testing.T, hook *exportIntercept) {
	t.Helper()
	select {
	case <-hook.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("export never reached actual SQL snapshot barrier")
	}
}
