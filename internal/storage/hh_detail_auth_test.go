//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"strings"
	"testing"
)

func TestMariaDBHHDetailAuthorizationDisposable(t *testing.T) {
	f := openApifyRuntimeFixture(t)
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(991305,'CI HH detail auth',991302,'CI title',?,'https://hh.ru/vacancy/123','hh','site','CI','CI','CI','CI')`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DELETE FROM vacancy_candidates WHERE id=991305"); err != nil {
			t.Error("detail auth cleanup")
		}
	})
	v := f.v
	v.CandidateID = 991305
	v.TitleRevision = 1
	v.VacancyURL = "https://hh.ru/vacancy/123"
	if key, err := f.repo.Settings.ResolveOperation(f.ctx, v); err != nil || key == "" {
		t.Fatal("owned candidate not authorized", err)
	}
	runtime := runtimeService(f, &runtimeProvider{})
	if profile, err := runtime.ResolveProfile(f.ctx, v, "CI detail profile", apify.HHOptions{Detail: true}); err != nil || profile != "free" {
		t.Fatal("initial profile", profile, err)
	}
	for _, q := range []string{"UPDATE vacancy_candidates SET title_revision=2 WHERE id=991305", "UPDATE vacancy_candidates SET vacancy_url='https://hh.ru/vacancy/456' WHERE id=991305", "UPDATE vacancy_candidates SET company_id=NULL WHERE id=991305", "UPDATE vacancy_candidates SET source_id='companySite' WHERE id=991305"} {
		if _, err := f.db.ExecContext(f.ctx, q); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.Settings.ResolveOperation(f.ctx, v); err == nil {
			t.Fatal("stale detail grant survived mutation")
		}
		if _, err := f.db.ExecContext(f.ctx, "UPDATE vacancy_candidates SET title_revision=1,vacancy_url='https://hh.ru/vacancy/123',company_id=991302,source_id='hh' WHERE id=991305"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMariaDBHHDetailProfileRecoveryDisposable(t *testing.T) {
	f := openApifyRuntimeFixture(t)
	if _, err := f.repo.Settings.SaveKey(f.ctx, f.v.Generation, apifysettings.KeyInput{Profile: "starter", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Settings.SaveSettings(f.ctx, f.v.Generation, apifysettings.SettingsInput{Enabled: true, PaidAllowed: true, Confirmed: true, Revision: 3}); err != nil {
		t.Fatal(err)
	}
	v := f.v
	v.SettingsRevision = 4
	v.Profile = "starter"
	p := &runtimeProvider{account: func(ctx context.Context) (apify.BillingAccount, error) {
		a, err := (&runtimeProvider{}).Account(ctx)
		a.PlanPriceUSD = 19
		a.PlanCreditsUSD = 19
		a.ProviderLimitUSD = 19
		return a, err
	}, start: func(context.Context) (apify.ActorRun, error) { return apify.ActorRun{}, errors.New("CI lost ACK") }}
	runtime := runtimeService(f, p)
	if _, err := runtime.Execute(f.ctx, v, "CI selected profile", apify.HHOptions{}); !errors.Is(err, apifyruns.ErrUncertain) {
		t.Fatal("profile fixture", err)
	}
	v.Profile = "free"
	profile, err := runtime.ResolveProfile(f.ctx, v, "CI selected profile", apify.HHOptions{})
	if err != nil || profile != "starter" || p.starts.Load() != 1 {
		t.Fatal("profile replay lost durable selection", profile, err)
	}
}
