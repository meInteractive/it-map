//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"testing"

	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
)

func TestMariaDBApifyHistoricalGuardDisposable(t *testing.T) {
	for _, tc := range []struct {
		name, state, run, dataset, account string
		charge                             any
	}{
		{"unknown", "uncertain", "", "", "CIHistory", nil}, {"running", "running", "CIRun", "CIData", "CIHistory", nil},
		{"missing_cost", "succeeded", "CIRun", "CIData", "CIHistory", nil}, {"contradictory", "running", "CIRun", "CIData", "CIHistory", 0.1},
		{"missing_run", "succeeded", "", "CIData", "CIHistory", 0.1}, {"bad_run", "succeeded", "bad-run", "CIData", "CIHistory", 0.1},
		{"missing_dataset", "succeeded", "CIRun", "", "CIHistory", 0.1}, {"bad_dataset", "succeeded", "CIRun", "bad-data", "CIHistory", 0.1},
		{"bad_account", "succeeded", "CIRun", "CIData", "bad-account", 0.1},
		{"bad_generation", "succeeded", "CIRun", "CIData", "CIHistory", 0.1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openApifyRuntimeFixture(t)
			p := &runtimeProvider{}
			service := runtimeService(f, p)
			options := apify.HHOptions{}
			if _, err := service.Execute(f.ctx, f.v, "CI existing", options); err != nil {
				t.Fatal("seed existing operation", err)
			}
			old := "11111111-2222-4333-8444-555555555555"
			if old == f.v.Generation {
				old = "11111111-2222-4333-8444-555555555556"
			}
			insertGeneration := old
			if tc.name == "bad_generation" {
				insertGeneration = "not-a-valid-generation"
			}
			_, err := f.db.ExecContext(f.ctx, `INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('CI_historical_paid',?,'CI_old','free',?,'CIhash','CIstart','CIend',0.25,?,?,?,?,1,1)`, insertGeneration, tc.account, tc.charge, tc.state, tc.run, tc.dataset)
			if err != nil {
				t.Fatal("insert historical evidence", err)
			}
			existing, err := service.Execute(f.ctx, f.v, "CI existing", options)
			if err != nil || existing.State != "succeeded" || p.runs.Load() != 1 || p.starts.Load() != 1 {
				t.Fatal("known current operation did not reconcile", err)
			}
			if _, err = service.Execute(f.ctx, f.v, "CI new", options); !errors.Is(err, apifyruns.ErrHistoricalUnresolved) || p.starts.Load() != 1 {
				t.Fatal("historical unknown allowed new POST", err)
			}
			var count int
			if err = f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&count); err != nil || count != 2 {
				t.Fatal("blocked reserve changed ledger", err)
			}
			if _, err = f.db.ExecContext(f.ctx, "UPDATE apify_runs SET state='succeeded',charged_usd=0.1,run_id='CIResolved',dataset_id='CIData',account_id='CIHistory',generation=? WHERE id='CI_historical_paid'", old); err != nil {
				t.Fatal(err)
			}
			if _, err = service.Execute(f.ctx, f.v, "CI new", options); err != nil || p.starts.Load() != 2 {
				t.Fatal("known historic cost blocked new operation", err)
			}
		})
	}
}
