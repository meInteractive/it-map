//go:build mariadb && v39restore && schemainit

package storage

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"itmap/internal/dbschema"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
