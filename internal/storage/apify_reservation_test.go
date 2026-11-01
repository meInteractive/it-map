//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

func TestMariaDBApifyReservationDisposable(t *testing.T) {
	ctx, db := schedulerDB(t)
	var count int
	if e := db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM apify_runs)+(SELECT COUNT(*) FROM apify_settings)+(SELECT COUNT(*) FROM app_catalog)+(SELECT COUNT(*) FROM company_identity WHERE id=991301)").Scan(&count); e != nil || count != 0 {
		t.Fatal("reservation fixture must be empty", e)
	}
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM apify_runs", "DELETE FROM apify_settings WHERE id=1", "DELETE FROM vacancy_source_parsing WHERE company_id=991301", "DELETE FROM company_identity WHERE id=991301", "DELETE FROM app_catalog WHERE id=1"} {
			if _, e := db.ExecContext(context.Background(), q); e != nil {
				t.Error("reservation cleanup")
			}
		}
	})
	if _, e := db.ExecContext(ctx, "INSERT INTO company_identity(id,company_name) VALUES(991301,'CI Apify Reserve')"); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, "INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[{"name":"CI Apify Reserve","vacancySources":{"hh":"https://hh.ru/employer/123"}}],"agencies":[]}`); e != nil {
		t.Fatal(e)
	}
	settings := apifysettings.NewRepository(db, "CI_SECRET")
	generation, e := settings.Generation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("r", 16), Revision: 0}); e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveSettings(ctx, generation, apifysettings.SettingsInput{Enabled: true, Revision: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveHHSource(ctx, generation, 991301, apifysettings.HHSourceInput{SourceURL: "https://hh.ru/employer/123", Enabled: true, Revision: 0}); e != nil {
		t.Fatal(e)
	}
	account := map[string]any{"accountId": "CIReserve", "planPriceUsd": 0, "planCreditsUsd": 5, "providerLimitUsd": 5, "providerUsedUsd": 0, "periodStart": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), "periodEnd": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	saveAccount := func() {
		raw, _ := json.Marshal(account)
		if _, e := db.ExecContext(ctx, "UPDATE apify_settings SET free_account_json=? WHERE id=1", string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	saveAccount()
	repo := apifyruns.Repository{DB: db, Settings: settings}
	version := apifysettings.OperationVersion{Generation: generation, SettingsRevision: 2, CompanyID: 991301, SourceURL: "https://hh.ru/employer/123", SourceRevision: 1, Profile: "free"}
	accountRaw, _ := json.Marshal(account)
	var snapshot apify.BillingAccount
	if e = json.Unmarshal(accountRaw, &snapshot); e != nil {
		t.Fatal(e)
	}
	snapshot.ProviderUsedUSD = 2
	snapshot.CheckedAt = time.Now().UnixMilli()
	if _, e = repo.StoreAccount(ctx, version, snapshot); e != nil {
		t.Fatal("store account", e)
	}
	snapshot.ProviderUsedUSD = 1
	if saved, e := repo.StoreAccount(ctx, version, snapshot); e != nil || saved.ProviderUsedUSD != 2 {
		t.Fatal("account usage decreased", e)
	}
	type result struct {
		r apifyruns.Reservation
		e error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := repo.Reserve(ctx, version, "CI same operation")
			results <- result{r, e}
		}()
	}
	wg.Wait()
	close(results)
	created, replayed := 0, 0
	id := ""
	for result := range results {
		if result.e != nil {
			t.Fatal("concurrent reserve", result.e)
		}
		if id != "" && id != result.r.ID {
			t.Fatal("duplicate reservation")
		}
		id = result.r.ID
		if result.r.Existing {
			replayed++
		} else {
			created++
		}
	}
	if created != 1 || replayed != 1 {
		t.Fatal("operation not idempotent")
	}
	if _, e = repo.Reserve(ctx, version, "CI competing operation"); !errors.Is(e, apifyruns.ErrBusy) {
		t.Fatal("single flight not enforced", e)
	}
	if _, e = db.ExecContext(ctx, "UPDATE apify_runs SET created_at=? WHERE id=?", time.Now().UnixMilli()-301000, id); e != nil {
		t.Fatal(e)
	}
	account["providerUsedUsd"] = 4.9
	saveAccount()
	if _, e = repo.Reserve(ctx, version, "CI insufficient budget"); !errors.Is(e, apifyruns.ErrBudget) {
		t.Fatal("overspend accepted", e)
	}
	stale := version
	stale.SettingsRevision--
	if _, e = repo.Reserve(ctx, stale, "CI stale settings"); !errors.Is(e, apifysettings.ErrChanged) {
		t.Fatal("stale settings accepted", e)
	}
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&count); e != nil || count != 1 {
		t.Fatal("denial created extra reservations", e)
	}
}
