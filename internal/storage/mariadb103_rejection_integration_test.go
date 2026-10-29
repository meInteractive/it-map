//go:build mariadb && mariadb103

package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/dbschema"
)

func TestMariaDB103RejectionDisposable(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("MariaDB 10.3 rejection requires disposable GitHub CI")
		}
		t.Skip("MariaDB 10.3 rejection requires disposable GitHub CI")
	}
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		t.Fatal("refusing non-disposable MariaDB target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open disposable MariaDB")
	}
	defer db.Close()
	var version, nonce string
	if db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version) != nil || !strings.HasPrefix(version, "10.3.32-MariaDB") {
		t.Fatal("required exact MariaDB 10.3.32 server unavailable")
	}
	if db.QueryRowContext(ctx, "SELECT generation FROM itmap_ci_guard.ownership WHERE database_name='itmap_ci'").Scan(&nonce) != nil || requireDisposableGeneration(os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION"), nonce) != nil {
		t.Fatal("disposable database ownership mismatch")
	}
	// Маркер создаёт CI root. Тестовый пользователь не может его переписать.
	_, err = db.ExecContext(ctx, "UPDATE itmap_ci_guard.ownership SET generation=generation WHERE database_name='itmap_ci'")
	var denied *mysql.MySQLError
	if !errors.As(err, &denied) || denied.Number != 1142 {
		t.Fatal("CI guard must deny writer UPDATE")
	}
	counts := func() [4]int {
		var n [4]int
		if db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()),
 (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()),
 (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE()),
 (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE())`).Scan(&n[0], &n[1], &n[2], &n[3]) != nil {
			t.Fatal("cannot inspect disposable schema counters")
		}
		return n
	}
	before := counts()
	if before[0] != 72 || before[1] != 606 || before[3] != 0 {
		t.Fatal("expected schema import to stop at first long unique index")
	}
	opened, err := Open(ctx, Settings{Host: "127.0.0.1", Port: "3306", Database: "itmap_ci", User: "itmap_ci", MaxOpen: 1, MaxIdle: 1}, cfg.Passwd)
	if opened != nil {
		opened.Close()
		t.Fatal("unsupported server returned a runtime DB pool")
	}
	if !errors.Is(err, dbschema.ErrUnsupportedServer) {
		t.Fatal("runtime must reject server before baseline validation")
	}
	result, err := dbschema.Initialize(ctx, db)
	if !errors.Is(err, dbschema.ErrUnsupportedServer) || result.Applied != 0 || result.Ready || result.Seeded {
		t.Fatal("schema-init must reject unsupported server before DDL")
	}
	if before != counts() {
		t.Fatal("version rejection changed schema metadata")
	}
	var generations int
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM app_runtime_generation").Scan(&generations) != nil || generations != 0 {
		t.Fatal("unsupported server must not seed runtime generation")
	}
	t.Log("PASS: exact MariaDB 10.3.32; unsupported long unique schema; runtime/schema-init reject before baseline/DDL, unchanged metadata and no generation")
}
