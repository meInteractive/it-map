//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type apifyRuntimeFixture struct {
	ctx  context.Context
	db   *sql.DB
	repo apifyruns.Repository
	v    apifysettings.OperationVersion
}

func openApifyRuntimeFixture(t *testing.T) apifyRuntimeFixture {
	t.Helper()
	ctx, db := schedulerDB(t)
	var n int
	if e := db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM apify_runs)+(SELECT COUNT(*) FROM apify_settings)+(SELECT COUNT(*) FROM app_catalog)+(SELECT COUNT(*) FROM company_identity WHERE id=991302)").Scan(&n); e != nil || n != 0 {
		t.Fatal("runtime fixture must be empty", e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, args...); e != nil {
			t.Fatal("runtime fixture SQL", e)
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM apify_runs", "DELETE FROM apify_account_usage WHERE account_id='CIRuntime'", "DELETE FROM apify_settings", "DELETE FROM vacancy_source_parsing WHERE company_id=991302", "DELETE FROM company_identity WHERE id=991302", "DELETE FROM app_catalog"} {
			if _, e := db.ExecContext(context.Background(), q); e != nil {
				t.Error("runtime fixture cleanup")
			}
		}
	})
	exec("INSERT INTO company_identity(id,company_name) VALUES(991302,'CI Apify Runtime')")
	exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[{"name":"CI Apify Runtime","vacancySources":{"hh":"https://hh.ru/employer/456"}}],"agencies":[]}`)
	settings := apifysettings.NewRepository(db, "CI_SECRET")
	g, e := settings.Generation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveKey(ctx, g, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("r", 16), Revision: 0}); e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveSettings(ctx, g, apifysettings.SettingsInput{Enabled: true, Revision: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = settings.SaveHHSource(ctx, g, 991302, apifysettings.HHSourceInput{SourceURL: "https://hh.ru/employer/456", Enabled: true, Revision: 0}); e != nil {
		t.Fatal(e)
	}
	return apifyRuntimeFixture{ctx, db, apifyruns.Repository{DB: db, Settings: settings}, apifysettings.OperationVersion{Generation: g, SettingsRevision: 2, CompanyID: 991302, SourceURL: "https://hh.ru/employer/456", SourceRevision: 1, Profile: "free"}}
}

type runtimeProvider struct {
	starts  atomic.Int64
	runs    atomic.Int64
	start   func(context.Context) (apify.ActorRun, error)
	account func(context.Context) (apify.BillingAccount, error)
	poll    func(context.Context, string) (apify.ActorRun, error)
}

func (p *runtimeProvider) Account(ctx context.Context) (apify.BillingAccount, error) {
	if p.account != nil {
		return p.account(ctx)
	}
	return apify.BillingAccount{AccountID: "CIRuntime", PlanCreditsUSD: 5, ProviderLimitUSD: 5, PeriodStart: time.Now().UTC().Truncate(time.Hour).Add(-time.Hour).Format(time.RFC3339), PeriodEnd: time.Now().UTC().Truncate(time.Hour).Add(2 * time.Hour).Format(time.RFC3339), CheckedAt: time.Now().UnixMilli()}, nil
}
func (p *runtimeProvider) Start(ctx context.Context, source string, o apify.HHOptions) (apify.ActorRun, error) {
	p.starts.Add(1)
	if source != "https://hh.ru/employer/456" {
		return apify.ActorRun{}, errors.New("wrong source")
	}
	if p.start != nil {
		return p.start(ctx)
	}
	return runtimeRun("RUNNING", "null"), nil
}
func (p *runtimeProvider) Run(ctx context.Context, id string) (apify.ActorRun, error) {
	p.runs.Add(1)
	if p.poll != nil {
		return p.poll(ctx, id)
	}
	return runtimeRun("SUCCEEDED", "0.08"), nil
}
func runtimeRun(status, cost string) apify.ActorRun {
	return apify.ActorRun{ID: "CIRun123", DefaultDatasetID: "CIData123", Status: status, Raw: json.RawMessage(`{"id":"CIRun123","defaultDatasetId":"CIData123","userId":"CIRuntime","status":"` + status + `","usageTotalUsd":` + cost + `}`)}
}
func runtimeService(f apifyRuntimeFixture, p *runtimeProvider) apifyruns.Service {
	return apifyruns.Service{Repository: f.repo, NewClient: func(string) (apifyruns.Provider, error) { return p, nil }}
}

func TestMariaDBApifyRuntimeDisposable(t *testing.T) {
	t.Run("concurrent replay grants one POST and monotonic settlement", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{}
		s := runtimeService(f, p)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, e := s.Execute(f.ctx, f.v, "CI operation", apify.HHOptions{})
				if e != nil && !errors.Is(e, apifyruns.ErrUncertain) {
					t.Error(e)
				}
			}()
		}
		wg.Wait()
		if p.starts.Load() != 1 {
			t.Fatal("duplicate POST", p.starts.Load())
		}
		got, e := s.Execute(f.ctx, f.v, "CI operation", apify.HHOptions{})
		if e != nil || got.State != "succeeded" || got.ChargedUSD == nil || *got.ChargedUSD != 0.08 {
			t.Fatal("recovery did not settle", e)
		}
		p.poll = func(context.Context, string) (apify.ActorRun, error) { return runtimeRun("RUNNING", "null"), nil }
		got, e = s.Reconcile(f.ctx, f.v, "CI operation", apify.HHOptions{})
		if e != nil || got.State != "succeeded" || got.ChargedUSD == nil || *got.ChargedUSD != 0.08 || p.starts.Load() != 1 {
			t.Fatal("late response erased charge/state", e)
		}
	})
	t.Run("lost acknowledgement crash and restarted service never retry", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{start: func(context.Context) (apify.ActorRun, error) { return apify.ActorRun{}, errors.New("CI lost ACK") }}
		_, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI lost", apify.HHOptions{})
		if e == nil {
			t.Fatal("lost acknowledgement accepted")
		}
		_, e = runtimeService(f, p).Execute(f.ctx, f.v, "CI lost", apify.HHOptions{})
		if !errors.Is(e, apifyruns.ErrUncertain) || p.starts.Load() != 1 {
			t.Fatal("uncertain POST replayed", e)
		}
		var cost sql.NullFloat64
		var state string
		if e = f.db.QueryRowContext(f.ctx, "SELECT state,charged_usd FROM apify_runs").Scan(&state, &cost); e != nil || state != "uncertain" || cost.Valid {
			t.Fatal("uncertainty released reserve", e)
		}
	})
	t.Run("rotation during network fences response and preserves reserve", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{}
		p.start = func(context.Context) (apify.ActorRun, error) {
			_, e := f.repo.Settings.SaveKey(f.ctx, f.v.Generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: 2})
			if e != nil {
				t.Fatal(e)
			}
			return runtimeRun("SUCCEEDED", "0.01"), nil
		}
		_, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI rotate", apify.HHOptions{})
		if !errors.Is(e, apifysettings.ErrChanged) {
			t.Fatal("late result survived rotation", e)
		}
		var state, id string
		var cost sql.NullFloat64
		if e = f.db.QueryRowContext(f.ctx, "SELECT state,run_id,charged_usd FROM apify_runs").Scan(&state, &id, &cost); e != nil || state != "uncertain" || id != "" || cost.Valid {
			t.Fatal("rotated result changed ledger", e)
		}
	})
	t.Run("account refresh cannot hold SQL locks and late revision is rejected", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{}
		p.account = func(context.Context) (apify.BillingAccount, error) {
			_, e := f.repo.Settings.SaveSettings(f.ctx, f.v.Generation, apifysettings.SettingsInput{Enabled: false, Revision: 2})
			if e != nil {
				t.Fatal(e)
			}
			return (&runtimeProvider{}).Account(f.ctx)
		}
		_, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI stale account", apify.HHOptions{})
		if !errors.Is(e, apifysettings.ErrChanged) || p.starts.Load() != 0 {
			t.Fatal("late account authorized start", e)
		}
	})
}

type apifyHTTPTransport func(*http.Request) (*http.Response, error)

func (f apifyHTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMariaDBApifyRuntimeHTTPDisposable(t *testing.T) {
	f := openApifyRuntimeFixture(t)
	var posts atomic.Int64
	var getRuns atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer apify_api_"+strings.Repeat("r", 16) {
			t.Error("wrong authorized key")
		}
		switch r.URL.Path {
		case "/v2/users/me":
			io.WriteString(w, `{"data":{"id":"CIRuntime","plan":{"monthlyBasePriceUsd":0,"monthlyUsageCreditsUsd":5}}}`)
		case "/v2/users/me/limits":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"monthlyUsageCycle": map[string]any{"startAt": time.Now().UTC().Truncate(time.Hour).Add(-time.Hour).Format(time.RFC3339), "endAt": time.Now().UTC().Truncate(time.Hour).Add(2 * time.Hour).Format(time.RFC3339)}, "current": map[string]any{"monthlyUsageUsd": 0}, "limits": map[string]any{"maxMonthlyUsageUsd": 5}}})
		case "/v2/actors/" + apify.Actor + "/runs":
			posts.Add(1)
			var input apify.StartInput
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&input) != nil || input.StartURL != "https://hh.ru/search/vacancy?employer_id=456&page=0&per_page=100" || r.URL.Query().Get("maxTotalChargeUsd") != "0.25" {
				t.Error("wrong server input/cap")
			}
			var state string
			if e := f.db.QueryRowContext(f.ctx, "SELECT state FROM apify_runs").Scan(&state); e != nil || state != "uncertain" {
				t.Error("POST preceded durable claim", e)
			}
			io.WriteString(w, `{"data":{"id":"CIRun123","defaultDatasetId":"CIData123","userId":"CIRuntime","status":"RUNNING"}}`)
		case "/v2/actor-runs/CIRun123":
			getRuns.Add(1)
			io.WriteString(w, `{"data":{"id":"CIRun123","defaultDatasetId":"CIData123","userId":"CIRuntime","status":"SUCCEEDED","usageTotalUsd":0.04}}`)
		default:
			t.Error("unexpected provider endpoint")
			http.Error(w, "CI unexpected endpoint", 400)
		}
	}))
	defer peer.Close()
	local, e := url.Parse(peer.URL)
	if e != nil {
		t.Fatal(e)
	}
	transport := http.DefaultTransport
	http.DefaultTransport = apifyHTTPTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.apify.com" {
			return nil, errors.New("external network forbidden in fixture")
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = local.Scheme
		u.Host = local.Host
		clone.URL = &u
		return transport.RoundTrip(clone)
	})
	defer func() { http.DefaultTransport = transport }()
	s := apifyruns.Service{Repository: f.repo}
	got, e := s.Execute(f.ctx, f.v, "CI HTTP", apify.HHOptions{})
	if e != nil || got.State != "running" {
		t.Fatal("HTTP start", e)
	}
	got, e = s.Execute(f.ctx, f.v, "CI HTTP", apify.HHOptions{})
	if e != nil || got.State != "succeeded" || got.ChargedUSD == nil || *got.ChargedUSD != 0.04 || posts.Load() != 1 || getRuns.Load() != 1 {
		t.Fatal("HTTP reconcile/replay", e)
	}
}
