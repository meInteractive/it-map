//go:build mariadb && catalogreads

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/catalog"
)

type catalogSnapshotHook struct {
	mu               sync.Mutex
	entered, release chan struct{}
	fired, readOnly  bool
	isolation        driver.IsolationLevel
}
type catalogSnapshotConnector struct {
	driver.Connector
	hook *catalogSnapshotHook
}

func (c catalogSnapshotConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &catalogSnapshotConn{Conn: conn, hook: c.hook}, nil
}

type catalogSnapshotConn struct {
	driver.Conn
	hook *catalogSnapshotHook
}

func (c *catalogSnapshotConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.hook.mu.Lock()
	c.hook.readOnly = opts.ReadOnly
	c.hook.isolation = opts.Isolation
	c.hook.mu.Unlock()
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *catalogSnapshotConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT id, company_name FROM company_identity") {
		c.hook.mu.Lock()
		first := !c.hook.fired
		c.hook.fired = true
		c.hook.mu.Unlock()
		if first {
			close(c.hook.entered)
			select {
			case <-c.hook.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func TestMariaDBCatalogReadSnapshotDisposable(t *testing.T) {
	f := openCatalogSQL(t)
	v := catalogFrames(t)[0]
	f.seed(v)
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_DSN"))
	if e != nil {
		t.Fatal("invalid guarded catalog reader")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		t.Fatal("invalid guarded catalog connector")
	}
	hook := &catalogSnapshotHook{entered: make(chan struct{}), release: make(chan struct{})}
	db := sql.OpenDB(catalogSnapshotConnector{Connector: connector, hook: hook})
	defer db.Close()
	type result struct {
		data json.RawMessage
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, e := (catalog.Repository{DB: db}).ListEntities(f.ctx, "Eugene", catalog.CompanyType, catalog.ListView)
		done <- result{data, e}
	}()
	select {
	case <-hook.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog read did not reach snapshot boundary")
	}
	// The source JSON was already read. Commit the catalog and personal state
	// together using another real connection before the next reader query.
	tx, e := f.writer.BeginTx(f.ctx, nil)
	if e != nil {
		close(hook.release)
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(f.ctx, `UPDATE app_catalog SET data_json='{"companies":[{"name":"CI new"}],"agencies":[]}' WHERE id=1`); e == nil {
		_, e = tx.ExecContext(f.ctx, "UPDATE company_user_state SET favorite=0,hidden=1 WHERE user_name='Eugene' AND company_name='CI A'")
	}
	if e != nil {
		_ = tx.Rollback()
		close(hook.release)
		t.Fatal("concurrent synthetic writer failed")
	}
	if e = tx.Commit(); e != nil {
		close(hook.release)
		t.Fatal(e)
	}
	close(hook.release)
	var got result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog snapshot read did not finish")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	var rows []struct {
		Name             string
		Favorite, Hidden bool
	}
	if e = json.Unmarshal(got.data, &rows); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range rows {
		if row.Name == "CI A" {
			found = true
			if !row.Favorite || row.Hidden {
				t.Fatal("read mixed old catalog with new personal state")
			}
		}
		if row.Name == "CI new" {
			t.Fatal("read mixed snapshots")
		}
	}
	if !found {
		t.Fatal("original catalog disappeared")
	}
	hook.mu.Lock()
	ro, iso := hook.readOnly, hook.isolation
	hook.mu.Unlock()
	if !ro || iso != driver.IsolationLevel(sql.LevelRepeatableRead) {
		t.Fatal("read transaction is not readonly repeatable read")
	}
	fresh, e := (catalog.Repository{DB: f.reader}).ListEntities(f.ctx, "Eugene", catalog.CompanyType, catalog.ListView)
	if e != nil || !strings.Contains(string(fresh), "CI new") || strings.Contains(string(fresh), "CI A") {
		t.Fatal("fresh read did not observe committed catalog")
	}
	t.Log("PASS: actual concurrent commit cannot mix catalog/personal snapshots; fresh reader sees new data")
}
