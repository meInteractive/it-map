//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

func TestMariaDBApifyKnownRecoveryDisposable(t *testing.T) {
	const id = "00000000000000000000000000000001"
	const oldHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const oldOperation = "go398:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, tc := range []struct {
		name string
		mode string
	}{
		{"rotated_token", "success"}, {"invalid_hash", "hash"}, {"invalid_operation", "operation"}, {"other_account", "account"}, {"nil_client", "nil_client"}, {"changed_row", "row"}, {"changed_settings", "settings"}, {"changed_generation", "generation"}, {"lost_ack", "lost_ack"}, {"before_commit", "before_commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openApifyRuntimeFixture(t)
			_, err := f.db.ExecContext(f.ctx, `INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?,?,?,'free','CIRuntime',?,'CIstart','CIend',0.25,'running','CIRun123','CIData123',1,1)`, id, f.v.Generation, oldOperation, oldHash)
			if err != nil {
				t.Fatal("synthetic ledger fixture", err)
			}
			expectedHash := oldHash
			if tc.mode == "hash" {
				expectedHash = "invalid"
				if _, e := f.db.ExecContext(f.ctx, "UPDATE apify_runs SET key_hash=? WHERE id=?", expectedHash, id); e != nil {
					t.Fatal(e)
				}
			}
			if tc.mode == "operation" {
				if _, e := f.db.ExecContext(f.ctx, "UPDATE apify_runs SET operation_key='invalid' WHERE id=?", id); e != nil {
					t.Fatal(e)
				}
			}
			if tc.mode == "generation" {
				defer func() {
					if _, e := f.db.ExecContext(context.Background(), "UPDATE app_runtime_generation SET generation=? WHERE id=1", f.v.Generation); e != nil {
						t.Error("generation cleanup failed")
					}
				}()
			}
			provider := &runtimeProvider{}
			if tc.mode == "account" {
				provider.account = func(context.Context) (apify.BillingAccount, error) {
					return apify.BillingAccount{AccountID: "CIOther"}, nil
				}
			}
			provider.poll = func(ctx context.Context, _ string) (apify.ActorRun, error) {
				switch tc.mode {
				case "row":
					if _, e := f.db.ExecContext(ctx, "UPDATE apify_runs SET updated_at=updated_at+1 WHERE id=?", id); e != nil {
						return apify.ActorRun{}, e
					}
				case "generation":
					next := "11111111-2222-4333-8444-555555555555"
					if next == f.v.Generation {
						next = "11111111-2222-4333-8444-555555555556"
					}
					if _, e := f.db.ExecContext(ctx, "UPDATE app_runtime_generation SET generation=? WHERE id=1", next); e != nil {
						return apify.ActorRun{}, e
					}
				case "settings":
					_, e := f.repo.Settings.SaveKey(ctx, f.v.Generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + "ssssssssssssssss", Revision: 2})
					if e != nil {
						return apify.ActorRun{}, e
					}
				}
				raw, _ := json.Marshal(map[string]any{"id": "CIRun123", "defaultDatasetId": "CIData123", "userId": "CIRuntime", "status": "SUCCEEDED", "finishedAt": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "usageTotalUsd": 0.08})
				return apify.ActorRun{ID: "CIRun123", DefaultDatasetID: "CIData123", Status: "SUCCEEDED", Raw: raw}, nil
			}
			service := runtimeService(f, provider)
			if tc.mode == "nil_client" {
				service.NewClient = func(string) (apifyruns.Provider, error) { return nil, nil }
			}
			if tc.mode == "lost_ack" || tc.mode == "before_commit" {
				repo, flag := billingCommitRepositoryMode(t, f.v.Generation, "UPDATE apify_runs SET state=?,charged_usd=?", tc.mode == "before_commit")
				service.Repository = apifyruns.Repository{DB: repo.DB, Settings: apifysettings.NewRepository(repo.DB, "CI_SECRET")}
				defer func() {
					if !flag.Load() {
						t.Error("commit fault not triggered")
					}
				}()
			}
			err = service.ReconcileKnownRecovery(f.ctx, f.v.Generation, id)
			var state string
			var cost sql.NullFloat64
			var keyhash string
			if e := f.db.QueryRowContext(f.ctx, "SELECT state,charged_usd,key_hash FROM apify_runs WHERE id=?", id).Scan(&state, &cost, &keyhash); e != nil {
				t.Fatal(e)
			}
			if provider.starts.Load() != 0 || keyhash != expectedHash {
				t.Fatal("recovery changed credential identity or started Actor")
			}
			switch tc.mode {
			case "success":
				if err != nil || state != "succeeded" || !cost.Valid || cost.Float64 != 0.08 || provider.runs.Load() != 1 {
					t.Fatal("verified recovery failed", err)
				}
			case "hash", "operation", "nil_client":
				if !errors.Is(err, apifyruns.ErrRecoveryEvidence) || cost.Valid || provider.runs.Load() != 0 {
					t.Fatal("corrupt ledger accepted", err)
				}
			case "account":
				if !errors.Is(err, apifyruns.ErrRecoveryEvidence) || cost.Valid || provider.runs.Load() != 0 {
					t.Fatal("different account accepted", err)
				}
			case "row", "settings":
				if !errors.Is(err, apifyruns.ErrRecoveryChanged) || cost.Valid {
					t.Fatal("changed state accepted", err)
				}
			case "generation":
				if !errors.Is(err, apifysettings.ErrGeneration) || cost.Valid {
					t.Fatal("changed generation accepted", err)
				}
			case "lost_ack":
				if !errors.Is(err, apifyruns.ErrRecoveryCommitUnknown) || !cost.Valid || state != "succeeded" {
					t.Fatal("lost commit ACK not preserved", err)
				}
				if again := service.ReconcileKnownRecovery(f.ctx, f.v.Generation, id); !errors.Is(again, apifyruns.ErrRecoveryEvidence) || provider.runs.Load() != 1 || provider.starts.Load() != 0 {
					t.Fatal("replay after lost ACK repeated provider call", again)
				}
			case "before_commit":
				if !errors.Is(err, apifyruns.ErrRecoveryCommitUnknown) || cost.Valid || state != "running" {
					t.Fatal("failed commit changed ledger", err)
				}
			}
		})
	}
}
