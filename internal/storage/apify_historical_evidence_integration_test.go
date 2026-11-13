//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
)

func TestMariaDBApifyHistoricalEvidenceDisposable(t *testing.T) {
	ctx, db := schedulerDB(t)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&count); err != nil || count != 0 {
		t.Fatal("history fixture not empty")
	}
	var current string
	if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&current); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, "DELETE FROM apify_runs WHERE account_id='CIHistory'"); err != nil {
			t.Error("history cleanup failed")
		}
	})
	old := "11111111-2222-4333-8444-555555555555"
	if old == current {
		old = "11111111-2222-4333-8444-555555555556"
	}
	insert := func(id, generation, state, run, dataset string, cost any) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?, ?, ?, 'free', 'CIHistory','CIhash','CIstart','CIend',0.25,?,?,?,?,1,1)`, id, generation, "CIkey"+id, cost, state, run, dataset)
		if err != nil {
			t.Fatal("synthetic history insert failed", err)
		}
	}
	insert("00000000000000000000000000000001", old, "uncertain", "", "", nil)
	insert("00000000000000000000000000000002", old, "running", "CIRun1", "CIData1", nil)
	insert("00000000000000000000000000000003", old, "succeeded", "CIRun2", "CIData2", nil)
	insert("00000000000000000000000000000004", old, "failed", "CIRun3", "CIData3", 0.0)
	insert("00000000000000000000000000000005", current, "uncertain", "", "", nil)
	repo := apifyruns.Repository{DB: db}
	summary, err := repo.InspectHistoricalEvidence(ctx, current)
	if err != nil || summary.Unknown != 1 || summary.Reconcile != 2 || summary.TerminalCostRecorded != 1 {
		t.Fatal("history counts invalid", summary, err)
	}
	if _, err = repo.InspectHistoricalEvidence(ctx, old); !errors.Is(err, apifysettings.ErrGeneration) {
		t.Fatal("stale generation accepted", err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var locked string
	if err = holder.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&locked); err != nil {
		holder.Rollback()
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = repo.InspectHistoricalEvidence(blocked, current)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		holder.Rollback()
		t.Fatal("generation lock bypassed", err)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE apify_runs SET state='failed' WHERE id='00000000000000000000000000000001'"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.InspectHistoricalEvidence(ctx, current); !errors.Is(err, apifyruns.ErrHistoricalEvidenceIncompatible) || got != (apifyruns.HistoricalEvidence{}) {
		t.Fatal("malformed row accepted", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE apify_runs SET state='uncertain' WHERE id='00000000000000000000000000000001'"); err != nil {
		t.Fatal(err)
	}
	var q strings.Builder
	q.WriteString("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES ")
	args := make([]any, 0, 1025*3)
	for i := 0; i < 1025; i++ {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteString("(?,?,?,'free','CIHistory','CIhash','CIstart','CIend',0.25,'uncertain',1,1)")
		id := fmt.Sprintf("%032x", i+100)
		args = append(args, id, old, "CIkey"+id)
	}
	if _, err = db.ExecContext(ctx, q.String(), args...); err != nil {
		t.Fatal("synthetic limit insert failed", err)
	}
	if got, err := repo.InspectHistoricalEvidence(ctx, current); !errors.Is(err, apifyruns.ErrHistoricalEvidenceLimit) || got != (apifyruns.HistoricalEvidence{}) {
		t.Fatal("history limit did not fail closed", err)
	}
}
