//go:build mariadb && v39restore && consumerevaluation

package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"itmap/internal/backup"
	"itmap/internal/consumercontrol"
	"itmap/internal/consumermigration"
	"itmap/internal/consumerpreflight"
)

func TestMariaDBVersionedConsumerRoundtripDisposable(t *testing.T) {
	f := openRestoreFixture(t)
	oldGeneration := f.generation()
	repo := backup.Repository{DB: f.db}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, q := range []string{"DROP TABLE IF EXISTS go_consumer_schema_version", "DROP TABLE IF EXISTS go_consumer_control"} {
			if _, err := f.db.ExecContext(ctx, q); err != nil {
				t.Error("consumer fixture cleanup failed")
			}
		}
	})
	if err := consumermigration.InitializeForEvaluation(f.ctx, f.db); err != nil {
		t.Fatal("migration failed", err)
	}
	if err := consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, oldGeneration); err != nil {
		t.Fatal("migrated schema rejected", err)
	}
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE go_consumer_control SET revision=7,mode='worker',owner_digest=?,lease_until=? WHERE id=1", digest[:], time.Now().UnixMilli()+60000)
	if _, err = repo.Export(f.ctx); err == nil {
		t.Fatal("ordinary export accepted versioned schema")
	}
	if _, err = repo.ExportConsumerCandidateForEvaluation(f.ctx, oldGeneration); err == nil {
		t.Fatal("unversioned export accepted marker")
	}
	if _, err = repo.RestoreV39(f.ctx, f.body); err == nil {
		t.Fatal("ordinary restore accepted versioned schema")
	}
	f.exec("UPDATE go_consumer_schema_version SET status='incomplete'")
	if _, err = repo.ExportVersionedConsumerForEvaluation(f.ctx, oldGeneration); err == nil {
		t.Fatal("export accepted incomplete marker")
	}
	if _, err = repo.RestoreVersionedConsumerForEvaluation(f.ctx, f.body); err == nil {
		t.Fatal("restore accepted incomplete marker")
	}
	f.exec("UPDATE go_consumer_schema_version SET status='complete'")
	before, err := repo.ExportVersionedConsumerForEvaluation(f.ctx, oldGeneration)
	if err != nil {
		t.Fatal("versioned export failed", err)
	}
	if bytes.Contains(before.Bytes, []byte("go_consumer_control")) || bytes.Contains(before.Bytes, []byte("go_consumer_schema_version")) || bytes.Contains(before.Bytes, []byte(base64.StdEncoding.EncodeToString(digest[:]))) {
		t.Fatal("operational state leaked")
	}
	beforePayload, err := backup.ValidateV39(before.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows := restoreRecords(t, beforePayload.Storage.Tables)
	hook := &restoreFault{prefix: "UPDATE go_consumer_control SET generation="}
	faultDB := restoreFaultPool(f, hook)
	if _, err = (backup.Repository{DB: faultDB}).RestoreVersionedConsumerForEvaluation(f.ctx, f.body); err == nil || !hook.fired {
		t.Fatal("post-reset failure not injected")
	}
	if f.generation() != oldGeneration || f.count("SELECT COUNT(*) FROM go_consumer_control WHERE revision=7 AND mode='worker' AND owner_digest IS NOT NULL") != 1 || f.count("SELECT COUNT(*) FROM go_consumer_schema_version WHERE id=1 AND version=1 AND status='complete'") != 1 {
		t.Fatal("failure changed versioned control")
	}
	afterFailure, err := repo.ExportVersionedConsumerForEvaluation(f.ctx, oldGeneration)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := backup.ValidateV39(afterFailure.Bytes)
	if err != nil || !reflect.DeepEqual(beforeRows, restoreRecords(t, parsed.Storage.Tables)) {
		t.Fatal("failure changed application data")
	}
	result, err := repo.RestoreVersionedConsumerForEvaluation(f.ctx, f.body)
	if err != nil {
		t.Fatal("old archive restore failed", err)
	}
	if result.Generation == oldGeneration || f.generation() != result.Generation || f.count("SELECT COUNT(*) FROM go_consumer_control WHERE revision=1 AND mode='draining' AND owner_digest IS NULL AND lease_until=0") != 1 {
		t.Fatal("restore did not drain")
	}
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, result.Generation); err != nil {
		t.Fatal("restored versioned schema rejected", err)
	}
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := consumercontrol.Request{Generation: oldGeneration, Revision: 7, Origin: consumercontrol.WorkerRun, Token: token}
	if err = consumercontrol.GuardNewRun(f.ctx, tx, request); !errors.Is(err, consumercontrol.ErrStale) {
		tx.Rollback()
		t.Fatal("old owner accepted", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	file, err := repo.ExportVersionedConsumerForEvaluation(f.ctx, result.Generation)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := backup.ValidateV39(file.Bytes)
	if err != nil || !reflect.DeepEqual(restoreRecords(t, f.expected), restoreRecords(t, payload.Storage.Tables)) {
		t.Fatal("versioned export differs from restored oracle")
	}
	second, err := repo.RestoreVersionedConsumerForEvaluation(f.ctx, file.Bytes)
	if err != nil || second.Generation == result.Generation {
		t.Fatal("versioned roundtrip failed", err)
	}
	if err = consumerpreflight.CheckVersionedCandidate(f.ctx, f.db, second.Generation); err != nil {
		t.Fatal("second restore invalid", err)
	}
}
