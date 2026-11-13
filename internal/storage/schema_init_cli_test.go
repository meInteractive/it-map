//go:build mariadb && v39restore && schemainit

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"itmap/internal/dbschema"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

type initFaultConnector struct {
	driver.Connector
	fired *bool
}

func (c initFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return initFaultConn{Conn: conn, fired: c.fired}, nil
}

type initFaultConn struct {
	driver.Conn
	fired *bool
}

func (c initFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err == nil && strings.HasPrefix(query, "SELECT GET_LOCK") && !*c.fired {
		*c.fired = true
		_ = rows.Close()
		return nil, errors.New("synthetic lost lock response")
	}
	return rows, err
}

func TestMariaDBSchemaInitCLIDisposable(t *testing.T) {
	ctx, db := openBlankSchemaFixture(t)
	f := &restoreFixture{t: t, ctx: ctx, db: db}
	binary := filepath.Join(t.TempDir(), "itmap")
	cmd := exec.CommandContext(ctx, "go", "build", "-tags", "mariadb", "-o", binary, "../../cmd/itmap")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("schema CLI build failed: %v %s", e, out)
	}
	call := func() dbschema.InitResult {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, "schema-init", "--database", "itmap_ci", "--user", "itmap_ci")
		env := []string{}
		for _, item := range os.Environ() {
			if strings.HasPrefix(item, "DB_PASSWORD=") || strings.HasPrefix(item, "SESSION_SECRET=") || strings.HasPrefix(item, "SITE_PASSWORD=") || strings.HasPrefix(item, "AUTH_ENABLED=") || strings.HasPrefix(item, "APP_ENV=") {
				continue
			}
			env = append(env, item)
		}
		cmd.Env = append(env, "DB_PASSWORD=ci_only_disposable_writer", "APP_ENV=staging")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("actual offline schema CLI failed")
		}
		if bytes.Contains(out, []byte("ci_only_disposable_writer")) {
			t.Fatal("schema CLI leaked credentials")
		}
		var result dbschema.InitResult
		if json.Unmarshal(out, &result) != nil || !result.Ready {
			t.Fatal("invalid CLI result")
		}
		return result
	}
	first := call()
	if first.Applied != 217 || !first.Seeded {
		t.Fatal("CLI did not initialize blank database")
	}
	gen := f.generation()
	second := call()
	if second.Applied != 0 || second.Seeded || gen != f.generation() {
		t.Fatal("repeated CLI init changed generation")
	}
}
func TestMariaDBSchemaInitOwnershipDisposable(t *testing.T) {
	ctx, db := openBlankSchemaFixture(t)
	conn, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	hash := sha256.Sum256([]byte("itmap_ci"))
	name := "itmap.restore.v39:" + hex.EncodeToString(hash[:16])
	var claimed int
	if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&claimed); e != nil || claimed != 1 {
		t.Fatal("cannot hold shared init/restore ownership")
	}
	defer func() {
		var released sql.NullInt64
		_ = conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", name).Scan(&released)
	}()
	if _, e = dbschema.Initialize(ctx, db); !errors.Is(e, dbschema.ErrBusy) {
		t.Fatal("schema initialization crossed existing restore ownership", e)
	}
	snapshot, e := dbschema.Inspect(ctx, db)
	if e != nil || len(snapshot["tables"]) != 0 {
		t.Fatal("busy initialization executed DDL")
	}
}

func TestMariaDBSchemaInitSessionDisposable(t *testing.T) {
	ctx, db := openBlankSchemaFixture(t)
	db.SetMaxOpenConns(1)
	for _, poison := range []string{
		"SET SESSION autocommit=0",
		"START TRANSACTION",
		"DO GET_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)),0)",
	} {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var original int64
		if err = conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&original); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, poison); err != nil {
			t.Fatal(err)
		}
		if err = conn.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = dbschema.Initialize(ctx, db); err == nil {
			t.Fatal("contaminated initialization session accepted")
		}
		var next int64
		var autocommit int
		var owner sql.NullInt64
		if err = db.QueryRowContext(ctx, "SELECT CONNECTION_ID(),@@autocommit,IS_USED_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)))").Scan(&next, &autocommit, &owner); err != nil || next == original || autocommit != 1 || owner.Valid {
			t.Fatal("contaminated init session was reused", err)
		}
		snapshot, err := dbschema.Inspect(ctx, db)
		if err != nil || len(snapshot["tables"]) != 0 {
			t.Fatal("rejected initialization executed DDL", err)
		}
	}
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		t.Fatal("unsafe fault target")
	}
	cfg.InterpolateParams = true
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	faultDB := sql.OpenDB(initFaultConnector{Connector: connector, fired: &fired})
	defer faultDB.Close()
	faultDB.SetMaxOpenConns(1)
	var original, next int64
	if err = faultDB.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err = dbschema.Initialize(ctx, faultDB); err == nil || !fired {
		t.Fatal("lost init lock response was accepted", err)
	}
	var owner sql.NullInt64
	if err = faultDB.QueryRowContext(ctx, "SELECT CONNECTION_ID(),IS_USED_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)))").Scan(&next, &owner); err != nil || original == next || owner.Valid {
		t.Fatal("uncertain init lock session reused", err)
	}
	result, err := dbschema.Initialize(ctx, db)
	if err != nil || !result.Ready {
		t.Fatal("clean initialization rejected", err)
	}
}
