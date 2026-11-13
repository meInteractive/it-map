//go:build mariadb && v39restore && consumerevaluation

package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"itmap/internal/backup"
	"itmap/internal/consumercontrol"
)

// Только отдельная disposable CI MariaDB. Production-команда не включает
// consumerevaluation и не вызывает экспериментальную ветку восстановления.
func TestMariaDBConsumerRestoreCandidateDisposable(t *testing.T) {
	f := openRestoreFixture(t)
	oldGeneration := f.generation()
	f.exec(`CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_control"); err != nil {
			t.Error("consumer fixture cleanup failed")
		}
	})
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO go_consumer_control VALUES(1,?,7,'worker',?,?)", oldGeneration, digest[:], time.Now().UnixMilli()+60000)
	repo := backup.Repository{DB: f.db}
	if _, err = repo.RestoreV39(f.ctx, f.body); err == nil {
		t.Fatal("ordinary restore accepted unversioned schema")
	}
	archive, err := repo.ExportConsumerCandidateForEvaluation(f.ctx, oldGeneration)
	if err != nil {
		t.Fatal("candidate export failed", err)
	}
	before, err := backup.ValidateV39(archive.Bytes)
	if err != nil {
		t.Fatal("invalid candidate archive", err)
	}
	beforeRows := restoreRecords(t, before.Storage.Tables)
	f.exec("ALTER TABLE go_consumer_control ADD COLUMN ci_unknown BIGINT NULL")
	if _, err = repo.RestoreConsumerCandidateForEvaluation(f.ctx, f.body); err == nil {
		t.Fatal("unknown metadata accepted")
	}
	f.exec("ALTER TABLE go_consumer_control DROP COLUMN ci_unknown")
	// Ошибка после обеих записей сброса: исходные данные и оба singleton
	// должны остаться целыми, даже если SQL уже вернул успех драйверу.
	hook := &restoreFault{prefix: "UPDATE go_consumer_control SET generation="}
	faultDB := restoreFaultPool(f, hook)
	if _, err = (backup.Repository{DB: faultDB}).RestoreConsumerCandidateForEvaluation(f.ctx, f.body); err == nil || !hook.fired {
		t.Fatal("restore fault was not injected")
	}
	if f.generation() != oldGeneration || f.count("SELECT COUNT(*) FROM go_consumer_control WHERE revision=7 AND mode='worker' AND owner_digest IS NOT NULL") != 1 {
		t.Fatal("failed restore changed control state")
	}
	afterFailure, err := repo.ExportConsumerCandidateForEvaluation(f.ctx, oldGeneration)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := backup.ValidateV39(afterFailure.Bytes)
	if err != nil || !reflect.DeepEqual(beforeRows, restoreRecords(t, parsed.Storage.Tables)) {
		t.Fatal("failed restore changed application data")
	}
	result, err := repo.RestoreConsumerCandidateForEvaluation(f.ctx, f.body)
	if err != nil {
		t.Fatal("old archive restore failed", err)
	}
	if result.Generation == oldGeneration || f.generation() != result.Generation || f.count("SELECT COUNT(*) FROM go_consumer_control WHERE revision=1 AND mode='draining' AND owner_digest IS NULL AND lease_until=0") != 1 {
		t.Fatal("restore did not drain and rotate")
	}
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := consumercontrol.Request{Generation: oldGeneration, Revision: 7, Origin: consumercontrol.WorkerRun, Token: token}
	if err = consumercontrol.GuardNewRun(f.ctx, tx, old); !errors.Is(err, consumercontrol.ErrStale) {
		tx.Rollback()
		t.Fatal("old owner accepted", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	reexport, err := repo.ExportConsumerCandidateForEvaluation(f.ctx, result.Generation)
	if err != nil {
		t.Fatal("candidate re-export failed", err)
	}
	payload, err := backup.ValidateV39(reexport.Bytes)
	if err != nil || !reflect.DeepEqual(restoreRecords(t, f.expected), restoreRecords(t, payload.Storage.Tables)) {
		t.Fatal("restored rows differ from v39 oracle")
	}
	second, err := repo.RestoreConsumerCandidateForEvaluation(f.ctx, reexport.Bytes)
	if err != nil || second.Generation == result.Generation {
		t.Fatal("candidate roundtrip failed", err)
	}
	if f.count("SELECT COUNT(*) FROM go_consumer_control WHERE revision=1 AND mode='draining' AND owner_digest IS NULL AND lease_until=0") != 1 {
		t.Fatal("roundtrip revived consumer")
	}
}
