//go:build mariadb && v39restore && consumerevaluation

package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"itmap/internal/consumermigration"
	"itmap/internal/consumerpreflight"
)

func TestMariaDBConsumerMigrationEvaluationDisposable(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("migration requires disposable CI")
		}
		t.Skip("migration requires disposable CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &restoreFixture{t: t, ctx: ctx, db: db}
	var generation string
	if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, q := range []string{"DROP TABLE IF EXISTS go_consumer_schema_version", "DROP TABLE IF EXISTS go_consumer_control"} {
			if _, err := db.ExecContext(cleanup, q); err != nil {
				t.Error("test table cleanup failed")
			}
		}
		if _, err := db.ExecContext(cleanup, "UPDATE app_runtime_generation SET generation=? WHERE id=1", generation); err != nil {
			t.Error("generation cleanup failed")
		}
	})
	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal("synthetic fixture SQL failed", err)
		}
	}
	exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", strings.Repeat("z", 36))
	if err := consumermigration.InitializeForEvaluation(ctx, db); err == nil {
		t.Fatal("invalid generation accepted")
	}
	if count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('go_consumer_control','go_consumer_schema_version')") != 0 {
		t.Fatal("DDL executed before generation check")
	}
	exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", generation)
	hook := &restoreFault{prefix: "CREATE TABLE go_consumer_control"}
	faultDB := restoreFaultPool(f, hook)
	if err := consumermigration.InitializeForEvaluation(ctx, faultDB); err == nil || !hook.fired {
		t.Fatal("lost DDL response accepted")
	}
	if count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='go_consumer_control'") != 1 {
		t.Fatal("fault did not occur after DDL")
	}
	if err := consumermigration.InitializeForEvaluation(ctx, db); err == nil {
		t.Fatal("partial DDL retried automatically")
	}
	exec("DROP TABLE go_consumer_control")
	hook = &restoreFault{prefix: "INSERT INTO go_consumer_schema_version"}
	faultDB = restoreFaultPool(f, hook)
	if err := consumermigration.InitializeForEvaluation(ctx, faultDB); err == nil || !hook.fired {
		t.Fatal("marker insert fault accepted")
	}
	if count("SELECT COUNT(*) FROM go_consumer_control") != 0 || count("SELECT COUNT(*) FROM go_consumer_schema_version") != 0 {
		t.Fatal("singleton and marker did not roll back together")
	}
	if err := consumermigration.InitializeForEvaluation(ctx, db); err == nil {
		t.Fatal("empty intermediate state retried automatically")
	}
	exec("DROP TABLE go_consumer_schema_version")
	exec("DROP TABLE go_consumer_control")
	if err := consumermigration.InitializeForEvaluation(ctx, db); err != nil {
		t.Fatal("valid migration failed", err)
	}
	if err := consumerpreflight.CheckVersionedCandidate(ctx, db, generation); err != nil {
		t.Fatal("migrated state rejected", err)
	}
	exec("UPDATE go_consumer_control SET mode='draining',revision=2 WHERE id=1")
	if err := consumermigration.InitializeForEvaluation(ctx, db); err != nil {
		t.Fatal("idempotent check failed", err)
	}
	if count("SELECT COUNT(*) FROM go_consumer_control WHERE mode='draining' AND revision=2") != 1 {
		t.Fatal("retry reset consumer state")
	}
	exec("UPDATE go_consumer_schema_version SET status='incomplete' WHERE id=1")
	if err := consumermigration.InitializeForEvaluation(ctx, db); err == nil {
		t.Fatal("incomplete marker accepted")
	}
	exec("UPDATE go_consumer_schema_version SET status='complete' WHERE id=1")
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var held sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)),0)").Scan(&held); err != nil || !held.Valid || held.Int64 != 1 {
		t.Fatal("cannot own schema lock", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(CONCAT('itmap.restore.v39:',LEFT(SHA2(DATABASE(),256),32)))").Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			t.Error("lock cleanup failed", err)
		}
	}()
	if err = consumermigration.InitializeForEvaluation(ctx, db); !errors.Is(err, consumermigration.ErrBusy) {
		t.Fatal("concurrent schema ownership ignored", err)
	}
}
