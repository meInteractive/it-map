//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"testing"
	"time"

	"itmap/internal/dbschema"
)

func testHHBindingMetadata(t *testing.T, f *schedulerFixture) {
	t.Helper()
	f.exec(`CREATE TABLE go_hh_attempt_bindings (operation_id VARCHAR(255) NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, operation_key VARCHAR(80) NOT NULL, job_id VARCHAR(255) NOT NULL, attempt_id VARCHAR(255) NOT NULL, UNIQUE KEY hh_binding_identity (generation,operation_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_hh_attempt_bindings"); err != nil {
			t.Error("HH binding table cleanup failed")
		}
	})
	snapshot, err := dbschema.Inspect(f.ctx, f.db)
	if err != nil || dbschema.CheckHHBindingCandidate(snapshot, dbschema.HHBindingCandidateVersion) != nil {
		t.Fatal("binding metadata rejected", err)
	}
	if dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion) == nil {
		t.Fatal("old schema policy silently accepted binding table")
	}
	f.exec("INSERT INTO go_hh_attempt_bindings VALUES('CIop',?,'CIkey','CIjob','CIattempt')", f.repo.Generation)
	if _, err = f.db.ExecContext(f.ctx, "INSERT INTO go_hh_attempt_bindings VALUES('CIother',?,'CIkey','CIotherjob','CIotherattempt')", f.repo.Generation); err == nil {
		t.Fatal("duplicate operation identity accepted")
	}
	if _, err = f.db.ExecContext(f.ctx, "INSERT INTO go_hh_attempt_bindings VALUES('CIop',?,'CIotherkey','CIotherjob','CIotherattempt')", f.repo.Generation); err == nil {
		t.Fatal("duplicate operation id accepted")
	}
	f.exec("ALTER TABLE go_hh_attempt_bindings ADD COLUMN ci_unknown BIGINT NULL")
	snapshot, err = dbschema.Inspect(f.ctx, f.db)
	if err != nil || dbschema.CheckHHBindingCandidate(snapshot, dbschema.HHBindingCandidateVersion) == nil {
		t.Fatal("unknown binding column accepted", err)
	}
}
