//go:build mariadb && trustedcapture

package storage

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"itmap/internal/dbschema"
	"os"
	"testing"
)

func TestMariaDBTrustedSchemaCaptureDisposable(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("schema capture requires disposable GitHub CI")
		}
		t.Skip("schema capture requires disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	// information_schema trigger bodies require TRIGGER privilege. The guarded
	// CI writer owns that privilege; inspection itself uses only SELECT through
	// a Reader interface and an explicit read-only transaction.
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	snapshot, e := dbschema.Inspect(ctx, tx)
	if e != nil {
		t.Fatal(e)
	}
	if len(snapshot["tables"]) != 72 || len(snapshot["columns"]) != 606 || len(snapshot["triggers"]) != 22 {
		t.Fatal("unexpected reviewed schema inventory")
	}
	fks := map[string]bool{}
	for _, r := range snapshot["foreign_keys"] {
		if len(r) != 9 || r[0] == nil || r[1] == nil {
			t.Fatal("invalid FK metadata")
		}
		fks[*r[0]+":"+*r[1]] = true
	}
	if len(fks) != 50 {
		t.Fatal("unexpected trusted FK inventory")
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	policy := dbschema.Trusted()
	if len(policy.Metadata) > 0 {
		if e = dbschema.CheckFull(snapshot); e != nil {
			t.Fatal("trusted metadata drift", e)
		}
	}
	encoded := base64.StdEncoding.EncodeToString(dbschema.CanonicalJSON(snapshot))
	n := (len(encoded) + 5999) / 6000
	for i := 0; i < n; i++ {
		end := (i + 1) * 6000
		if end > len(encoded) {
			end = len(encoded)
		}
		fmt.Printf("CI_SCHEMA_META %d/%d %s\n", i, n, encoded[i*6000:end])
	}
}
