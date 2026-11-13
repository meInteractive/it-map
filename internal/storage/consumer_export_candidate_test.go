//go:build mariadb && profilewrites && v39exports

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/backup"
)

func testConsumerCandidateExport(t *testing.T) {
	f := openProfileSettingsFixture(t)
	repo := backup.Repository{DB: f.db}
	f.save("Demo", "CI synthetic consumer export", 0)
	before, err := repo.Export(f.ctx)
	if err != nil {
		t.Fatal("baseline export failed", err)
	}
	var generation string
	if err = f.db.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil {
		t.Fatal("generation unavailable")
	}
	_, err = f.db.ExecContext(f.ctx, `CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	if err != nil {
		t.Fatal("candidate table failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_control"); err != nil {
			t.Error("candidate cleanup failed")
		}
	})
	digest := strings.Repeat("Q", 32)
	_, err = f.db.ExecContext(f.ctx, "INSERT INTO go_consumer_control VALUES(1,?,5,'worker',?,?)", generation, []byte(digest), time.Now().UnixMilli()+60000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Export(f.ctx); err == nil {
		t.Fatal("ordinary export accepted unversioned candidate")
	}
	file, err := repo.ExportConsumerCandidateForEvaluation(f.ctx, generation)
	if err != nil {
		t.Fatal("candidate export failed", err)
	}
	if bytes.Contains(file.Bytes, []byte(digest)) || bytes.Contains(file.Bytes, []byte(base64.StdEncoding.EncodeToString([]byte(digest)))) || bytes.Contains(file.Bytes, []byte("go_consumer_control")) {
		t.Fatal("consumer operational state leaked into archive")
	}
	if _, err = backup.PrepareV39Restore(file.Bytes); err != nil {
		t.Fatal("candidate archive is not valid v39", err)
	}
	if !reflect.DeepEqual(exportPayload(t, before).Storage, exportPayload(t, file).Storage) {
		t.Fatal("candidate changed business export")
	}
	other := "11111111-2222-4333-8444-555555555555"
	if other == generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, other); err == nil {
		t.Fatal("stale generation accepted")
	}
	if _, err = f.db.ExecContext(f.ctx, "UPDATE go_consumer_control SET owner_digest=NULL WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, generation); err == nil {
		t.Fatal("invalid singleton accepted")
	}
	if _, err = f.db.ExecContext(f.ctx, "UPDATE go_consumer_control SET owner_digest=? WHERE id=1", []byte(digest)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ExecContext(f.ctx, "ALTER TABLE go_consumer_control ADD COLUMN ci_unknown BIGINT NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, generation); err == nil {
		t.Fatal("unknown consumer column accepted")
	}
	if _, err = f.db.ExecContext(f.ctx, "ALTER TABLE go_consumer_control DROP COLUMN ci_unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ExecContext(f.ctx, "CREATE TABLE ci_unknown_consumer_export (id BIGINT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE IF EXISTS ci_unknown_consumer_export"); err != nil {
			t.Error("unknown table cleanup failed")
		}
	})
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, generation); err == nil {
		t.Fatal("unknown table accepted")
	}
	if _, err = f.db.ExecContext(f.ctx, "DROP TABLE ci_unknown_consumer_export"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, generation); err != nil {
		t.Fatal("export did not recover after rejected schema", err)
	}
	conn, err := f.db.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var database string
	if err = conn.QueryRowContext(f.ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(database))
	key := "itmap.restore.v39:" + hex.EncodeToString(h[:16])
	var held sql.NullInt64
	if err = conn.QueryRowContext(f.ctx, "SELECT GET_LOCK(?,0)", key).Scan(&held); err != nil || !held.Valid || held.Int64 != 1 {
		t.Fatal("cannot hold schema lock", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			t.Error("lock cleanup failed", err)
		}
	}()
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, generation); !errors.Is(err, backup.ErrRestoreBusy) {
		t.Fatal("shared ownership ignored", err)
	}
}
