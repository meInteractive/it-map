//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/consumerpreflight"
	"itmap/internal/dbschema"
)

// Только тестовая схема: metadata и marker сами не разрешают миграцию.
func testConsumerVersionEvidence(t *testing.T, f *schedulerFixture) {
	t.Helper()
	if err := consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("missing marker table accepted", err)
	}
	f.exec(`CREATE TABLE go_consumer_schema_version (id BIGINT NOT NULL PRIMARY KEY, version BIGINT NOT NULL, status VARCHAR(16) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_schema_version"); err != nil {
			t.Error("marker table cleanup failed")
		}
	})
	snapshot, err := dbschema.Inspect(f.ctx, f.db)
	if err != nil || dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion) != nil {
		t.Fatal("versioned metadata rejected", err)
	}
	if err = consumerpreflight.CheckCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("unversioned check accepted versioned table", err)
	}
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("empty marker accepted", err)
	}
	f.exec("INSERT INTO go_consumer_schema_version VALUES(1,1,'incomplete')")
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("incomplete marker accepted", err)
	}
	f.exec("UPDATE go_consumer_schema_version SET status='complete',version=2 WHERE id=1")
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("unknown marker version accepted", err)
	}
	f.exec("UPDATE go_consumer_schema_version SET version=1 WHERE id=1")
	f.exec("INSERT INTO go_consumer_schema_version VALUES(2,1,'complete')")
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrSingleton) {
		t.Fatal("duplicate marker accepted", err)
	}
	f.exec("DELETE FROM go_consumer_schema_version WHERE id=2")
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); err != nil {
		t.Fatal("valid marker rejected", err)
	}
	f.exec("ALTER TABLE go_consumer_schema_version ADD COLUMN ci_unknown BIGINT NULL")
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumerpreflight.ErrMetadata) {
		t.Fatal("unknown marker column accepted", err)
	}
	f.exec("ALTER TABLE go_consumer_schema_version DROP COLUMN ci_unknown")
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = consumercontrol.GuardVersionedSingleton(f.ctx, tx, f.repo.Generation); err != nil {
		tx.Rollback()
		t.Fatal("marker guard rejected", err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, err = f.db.ExecContext(blocked, "UPDATE go_consumer_schema_version SET version=version WHERE id=1")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		tx.Rollback()
		t.Fatal("marker lock not held", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, f.repo.Generation); err != nil {
		t.Fatal("marker lock not released", err)
	}
	t.Run("hh_binding_metadata", func(t *testing.T) {
		testHHBindingMetadata(t, f.sub(t))
	})
}
