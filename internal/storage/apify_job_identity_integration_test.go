//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

func testApifyJobIdentityRecovery(t *testing.T) {
	t.Run("lost provider acknowledgement remains unknown across reconstructed key", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{start: func(context.Context) (apify.ActorRun, error) {
			return apify.ActorRun{}, errors.New("synthetic lost acknowledgement")
		}}
		key, err := apifyruns.HHJobKey(f.v.Generation, "CI stable job", "CI page 1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = runtimeService(f, p).Execute(f.ctx, f.v, key, apify.HHOptions{}); !errors.Is(err, apifyruns.ErrUncertain) {
			t.Fatal("lost provider acknowledgement accepted", err)
		}
		reconstructed, err := apifyruns.HHJobKey(f.v.Generation, "CI stable job", "CI page 1")
		if err != nil || reconstructed != key {
			t.Fatal("reconstructed key changed", err)
		}
		if _, err = runtimeService(f, p).Execute(f.ctx, f.v, reconstructed, apify.HHOptions{}); !errors.Is(err, apifyruns.ErrUncertain) || p.starts.Load() != 1 {
			t.Fatal("recovery sent another POST", err)
		}
		var row apifyruns.Record
		var cost sql.NullFloat64
		var reserved float64
		if err = f.db.QueryRowContext(f.ctx, "SELECT id,state,run_id,dataset_id,account_id,charged_usd,reserved_usd FROM apify_runs").Scan(&row.ID, &row.State, &row.RunID, &row.DatasetID, &row.AccountID, &cost, &reserved); err != nil {
			t.Fatal(err)
		}
		if cost.Valid || reserved <= 0 {
			t.Fatal("unknown operation lost its reserve")
		}
		evidence, err := apifyruns.ClassifyRecoveryEvidence(row)
		if err != nil || evidence != apifyruns.RecoveryUnknown {
			t.Fatal("lost response evidence misclassified", err)
		}
	})
	t.Run("lost reservation commit does not permit POST", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{}
		key, err := apifyruns.HHJobKey(f.v.Generation, "CI stable job", "CI page 1")
		if err != nil {
			t.Fatal(err)
		}
		wrapped, fired := billingCommitRepository(t, f.v.Generation, "INSERT INTO apify_runs(")
		s := runtimeService(f, p)
		s.Repository = apifyruns.Repository{DB: wrapped.DB, Settings: apifysettings.NewRepository(wrapped.DB, "CI_SECRET")}
		if _, err = s.Execute(f.ctx, f.v, key, apify.HHOptions{}); err == nil || !fired.Load() {
			t.Fatal("unknown commit was not injected")
		}
		reconstructed, err := apifyruns.HHJobKey(f.v.Generation, "CI stable job", "CI page 1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = runtimeService(f, p).Execute(f.ctx, f.v, reconstructed, apify.HHOptions{}); !errors.Is(err, apifyruns.ErrUncertain) || p.starts.Load() != 0 {
			t.Fatal("unknown reserve commit allowed POST", err)
		}
	})
}
