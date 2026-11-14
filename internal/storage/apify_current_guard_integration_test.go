//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"testing"

	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
)

func TestMariaDBApifyCurrentUnknownGuardDisposable(t *testing.T) {
	for _, tc := range []struct {
		name, state, run string
		charge           any
	}{
		{"starting", "starting", "", nil},
		{"uncertain", "uncertain", "", nil},
		{"whitespace", "uncertain", "   ", nil},
		{"tab", "uncertain", "\t", nil},
		{"invalid_id", "uncertain", "bad-run", nil},
		{"contradictory", "failed", "", 0.1},
		{"running_known", "running", "CIRunOther", nil},
		{"terminal_no_cost", "succeeded", "CIRunOther", nil},
		{"running_with_cost", "running", "CIRunOther", 0.1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openApifyRuntimeFixture(t)
			p := &runtimeProvider{}
			service := runtimeService(f, p)
			options := apify.HHOptions{}
			if _, err := service.Execute(f.ctx, f.v, "CI known current", options); err != nil {
				t.Fatal("cannot seed known run", err)
			}
			_, err := f.db.ExecContext(f.ctx, `INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('CI_current_unknown',?,'CI_unknown','free','CIHistory','CIhash','CIstart','CIend',0.25,?,?,?,'',1,1)`, f.v.Generation, tc.charge, tc.state, tc.run)
			if err != nil {
				t.Fatal("cannot seed current unknown", err)
			}
			known, err := service.Execute(f.ctx, f.v, "CI known current", options)
			if err != nil || known.State != "succeeded" || p.starts.Load() != 1 || p.runs.Load() != 1 {
				t.Fatal("known run did not reconcile", err)
			}
			if _, err = service.Execute(f.ctx, f.v, "CI different job", options); !errors.Is(err, apifyruns.ErrCurrentUnresolved) || p.starts.Load() != 1 {
				t.Fatal("old unknown allowed new POST", err)
			}
			var count int
			if err = f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&count); err != nil || count != 2 {
				t.Fatal("blocked reserve changed ledger", err)
			}
			// Подтверждённый исход моделируется только в одноразовой CI БД.
			if _, err = f.db.ExecContext(f.ctx, "UPDATE apify_runs SET state='succeeded',run_id='CIResolved',dataset_id='CIData',charged_usd=0.1 WHERE id='CI_current_unknown'"); err != nil {
				t.Fatal(err)
			}
			if _, err = service.Execute(f.ctx, f.v, "CI different job", options); err != nil || p.starts.Load() != 2 {
				t.Fatal("resolved operation still blocked new POST", err)
			}
		})
	}
}
