//go:build mariadb && v39restore && schemainit

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"itmap/internal/dbschema"
	"os"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func TestMariaDBSchemaInitRetryDisposable(t *testing.T) {
	policy := dbschema.Trusted()
	stages := map[string]string{"generation": "INSERT INTO app_runtime_generation"}
	for _, kind := range []string{"table", "index", "foreign_key", "trigger"} {
		for _, a := range policy.Actions {
			if a.Kind == kind {
				stages[kind] = a.SQL
				break
			}
		}
	}
	for name, prefix := range stages {
		t.Run(name, func(t *testing.T) {
			ctx, db := openBlankSchemaFixture(t)
			f := &restoreFixture{t: t, ctx: ctx, db: db}
			hook := &restoreFault{prefix: prefix}
			pool := restoreFaultPool(f, hook)
			if _, e := dbschema.Initialize(ctx, pool); e == nil || !hook.fired {
				t.Fatal("DDL/seed fault did not run/fail")
			}
			partial, e := dbschema.Inspect(ctx, db)
			if e != nil || len(partial["tables"]) == 0 {
				t.Fatal("DDL fault did not leave inspectable committed schema")
			}
			result, e := dbschema.Initialize(ctx, db)
			if e != nil || !result.Ready || !result.Seeded {
				t.Fatalf("safe schema retry failed: %+v %v", result, e)
			}
			gen := f.generation()
			again, e := dbschema.Initialize(ctx, db)
			if e != nil || again.Applied != 0 || again.Seeded || gen != f.generation() {
				t.Fatal("repeat schema retry changed state", e)
			}
		})
	}
	t.Run("unknown generation commit is inspected on retry", func(t *testing.T) {
		ctx, db := openBlankSchemaFixture(t)
		f := &restoreFixture{t: t, ctx: ctx, db: db}
		cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
			t.Fatal("unsafe seed commit fault target")
		}
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			t.Fatal(e)
		}
		pool := sql.OpenDB(schemaCommitConnector{connector})
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { pool.Close() })
		if _, e = dbschema.Initialize(ctx, pool); e == nil {
			t.Fatal("unknown seed commit was reported successful")
		}
		generation := f.generation()
		result, e := dbschema.Initialize(ctx, db)
		if e != nil || result.Applied != 0 || result.Seeded || generation != f.generation() {
			t.Fatal("unknown seed commit retry rotated/rewrote state", e)
		}
	})
}

type schemaCommitConnector struct{ driver.Connector }

func (c schemaCommitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return schemaCommitConn{conn}, nil
}

type schemaCommitConn struct{ driver.Conn }

func (c schemaCommitConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return schemaCommitTx{tx}, nil
}
func (c schemaCommitConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
}
func (c schemaCommitConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}
func (c schemaCommitConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}

type schemaCommitTx struct{ driver.Tx }

func (tx schemaCommitTx) Commit() error {
	if e := tx.Tx.Commit(); e != nil {
		return e
	}
	return errors.New("CI_UNKNOWN_SEED_COMMIT")
}
