//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
	"strings"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
)

type hhPublicFixture struct {
	calls int
	job   *hhpublic.Job
	after func()
}

func (p *hhPublicFixture) Fetch(context.Context, string) (*hhpublic.Job, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	return p.job, nil
}
func hhDetailBody() string {
	return "Задачи: развивать продукт и управлять командой. Требования: опыт управления, навыки планирования. " + strings.Repeat("CI full vacancy description. ", 15)
}
func hhDetailFixture(t *testing.T) (apifyRuntimeFixture, *schedulerFixture, *parser.JobLease, int64) {
	t.Helper()
	provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
	f, s, scan, executor := hhScanFixture(t, provider)
	if _, err := executor.Execute(f.ctx, scan); err != nil {
		t.Fatal("seed HH candidate", err)
	}
	var candidate int64
	var jobID, runID string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM vacancy_candidates WHERE company_id=991302").Scan(&candidate); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_jobs WHERE candidate_id=? AND type='classify_title'", candidate).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&runID); err != nil {
		t.Fatal(err)
	}
	lease, err := s.repo.ClaimJob(s.ctx, runID, parser.ClaimOptions{JobID: &jobID})
	if err != nil || lease == nil {
		t.Fatal("classification lease", err)
	}
	runner := &storedTitleRunner{answers: map[string]struct{ Type, Choice string }{"role_991303": {"choice", "match"}}}
	ai := parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_OWNER"}, Classifier: runner, Handoff: &parser.CandidateHandoff{Planner: parser.SQLCandidateHandoffPlanner{Owner: "CI_OWNER"}}}
	if _, err = ai.Execute(s.ctx, lease); err != nil {
		t.Fatal("classify HH candidate", err)
	}
	if err = s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_jobs WHERE candidate_id=? AND type='fetch_full_text'", candidate).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	lease, err = s.repo.ClaimJob(s.ctx, runID, parser.ClaimOptions{JobID: &jobID})
	if err != nil || lease == nil {
		t.Fatal("detail lease", err)
	}
	t.Cleanup(func() {
		if _, err := s.db.ExecContext(context.Background(), "DELETE FROM vacancy_detail_leases WHERE lease_key=?", "candidate:"+fmt.Sprint(candidate)); err != nil {
			t.Error("detail lease cleanup")
		}
	})
	return f, s, lease, candidate
}
func TestMariaDBHHDetailDisposable(t *testing.T) {
	t.Run("public snapshot works with Apify disabled and schedules extraction", func(t *testing.T) {
		f, s, lease, candidate := hhDetailFixture(t)
		if _, err := f.repo.Settings.SaveSettings(f.ctx, f.v.Generation, apifysettings.SettingsInput{Enabled: false, Revision: 2}); err != nil {
			t.Fatal(err)
		}
		public := &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody()}}
		result, err := (parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: public}).Execute(s.ctx, lease)
		if err != nil || result.SnapshotID == "" || result.Reused || public.calls != 1 || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=? AND content_status='valid'", candidate) != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=? AND type='extract_vacancy'", candidate) != 1 {
			t.Fatal("public detail readback/handoff", result, err)
		}
		result, err = (parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: public}).Execute(s.ctx, lease)
		if err != nil || !result.Reused || public.calls != 1 || s.count("SELECT COUNT(*) FROM vacancy_content_fetches WHERE candidate_id=?", candidate) != 1 {
			t.Fatal("cache hit used provider", result, err)
		}
	})
	t.Run("large snapshot parts assemble and cache without network", func(t *testing.T) {
		_, s, lease, candidate := hhDetailFixture(t)
		public := &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody() + strings.Repeat("CI 😀 full text. ", 15000)}}
		executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: public}
		result, err := executor.Execute(s.ctx, lease)
		if err != nil || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshot_parts WHERE snapshot_id=?", result.SnapshotID) < 2 {
			t.Fatal("large snapshot not split", result, err)
		}
		var snapshot parser.StoredRawSnapshot
		if err = lease.WithinOwned(s.ctx, func(tx *sql.Tx) error {
			var err error
			snapshot, err = parser.ReadRawSnapshot(s.ctx, tx, candidate, "https://hh.ru/vacancy/123", "hh")
			return err
		}); err != nil || !strings.Contains(snapshot.Markdown, "😀") || len(snapshot.Markdown) < 180000 {
			t.Fatal("composite SQL readback", err)
		}
		result, err = executor.Execute(s.ctx, lease)
		if err != nil || !result.Reused || public.calls != 1 {
			t.Fatal("composite cache spent provider budget", err)
		}
		oldID := result.SnapshotID
		s.exec("UPDATE vacancy_raw_snapshot_parts SET markdown_raw='CI corrupted part' WHERE snapshot_id=? AND part_index=0", oldID)
		result, err = executor.Execute(s.ctx, lease)
		if err != nil || result.SnapshotID == oldID || public.calls != 2 {
			t.Fatal("corrupted composite snapshot reused", result, err)
		}

	})
}

type hhDetailProvider struct {
	starts, reads int
	accounts      int
	afterAccount  func()
	item          json.RawMessage
	pending, lost bool
	afterRead     func()
}

func (p *hhDetailProvider) Account(ctx context.Context) (apify.BillingAccount, error) {
	p.accounts++
	if p.afterAccount != nil {
		p.afterAccount()
	}
	return (&runtimeProvider{}).Account(ctx)
}
func (p *hhDetailProvider) Start(_ context.Context, source string, o apify.HHOptions) (apify.ActorRun, error) {
	p.starts++
	if source != "https://hh.ru/vacancy/123" || !o.Detail {
		return apify.ActorRun{}, errors.New("wrong detail input")
	}
	if p.lost {
		return apify.ActorRun{}, errors.New("CI lost ACK")
	}
	if p.pending {
		return hhDetailRun("RUNNING"), nil
	}
	return hhDetailRun("SUCCEEDED"), nil
}
func (p *hhDetailProvider) Run(context.Context, string) (apify.ActorRun, error) {
	return hhDetailRun("SUCCEEDED"), nil
}
func hhDetailRun(status string) apify.ActorRun {
	return apify.ActorRun{ID: "HHDetailRun", DefaultDatasetID: "HHDetailData", Status: status, Raw: json.RawMessage(fmt.Sprintf(`{"id":"HHDetailRun","defaultDatasetId":"HHDetailData","userId":"CIRuntime","status":%q,"usageTotalUsd":0.02}`, status))}
}
func (p *hhDetailProvider) DatasetInfo(context.Context, string) (apify.DatasetMetadata, error) {
	return apify.DatasetMetadata{ItemCount: 1, ModifiedAt: "2020-01-01T00:00:00Z"}, nil
}
func (p *hhDetailProvider) DatasetItems(_ context.Context, id string, offset int64, clean bool) ([]json.RawMessage, error) {
	p.reads++
	if id != "HHDetailData" || offset != 0 || clean {
		return nil, errors.New("wrong detail dataset")
	}
	if p.afterRead != nil {
		p.afterRead()
	}
	return []json.RawMessage{p.item}, nil
}
func hhDetailRuntime(s *schedulerFixture, p *hhDetailProvider) apifyruns.Service {
	return apifyruns.Service{Repository: apifyruns.Repository{DB: s.db, Settings: apifysettings.NewRepository(s.db, "CI_SECRET")}, NewClient: func(string) (apifyruns.Provider, error) { return p, nil }}
}
func hhDetailItem(description string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"title": "CI_TITLE", "url": "https://hh.ru/vacancy/123", "vacancy_id": "123", "description_text": description})
	return raw
}

type hhDetailLateFailurePlanner struct{ seen bool }

func (p *hhDetailLateFailurePlanner) Next(ctx context.Context, tx *sql.Tx, j parser.Job) (*parser.CandidateStagePlan, error) {
	var n int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", j.CandidateID).Scan(&n) == nil && n > 0 {
		p.seen = true
	}
	return nil, errors.New("CI late snapshot failure")
}
func TestMariaDBHHDetailRecoveryDisposable(t *testing.T) {
	t.Run("Apify pending resumes known run and reuses terminal snapshot", func(t *testing.T) {
		_, s, lease, candidate := hhDetailFixture(t)
		p := &hhDetailProvider{item: hhDetailItem(hhDetailBody()), pending: true}
		executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER"}
		result, err := executor.Execute(s.ctx, lease)
		if !errors.Is(err, parser.ErrHHDetailPending) || !result.Pending || p.starts != 1 {
			t.Fatal("pending detail", result, err)
		}
		result, err = executor.Execute(s.ctx, lease)
		if err != nil || result.SnapshotID == "" || p.starts != 1 || p.reads != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=? AND type='extract_vacancy'", candidate) != 1 {
			t.Fatal("known detail recovery", result, err)
		}
		result, err = executor.Execute(s.ctx, lease)
		if err != nil || !result.Reused || p.starts != 1 || p.reads != 1 {
			t.Fatal("detail snapshot cache spent provider", err)
		}
	})
	t.Run("unknown detail acknowledgement never starts twice", func(t *testing.T) {
		_, s, lease, candidate := hhDetailFixture(t)
		p := &hhDetailProvider{lost: true}
		executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER"}
		for i := 0; i < 2; i++ {
			if _, err := executor.Execute(s.ctx, lease); !errors.Is(err, apifyruns.ErrUncertain) {
				t.Fatal("unknown detail lost reserve", err)
			}
		}
		if p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 {
			t.Fatal("unknown detail repeated POST")
		}
	})
	t.Run("invalid detail records safe error history without extraction", func(t *testing.T) {
		_, s, lease, candidate := hhDetailFixture(t)
		p := &hhDetailProvider{item: hhDetailItem("captcha " + hhDetailBody())}
		executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER"}
		if _, err := executor.Execute(s.ctx, lease); !errors.Is(err, parser.ErrHHDetailInvalid) {
			t.Fatal("invalid description accepted", err)
		}
		if s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 || s.count("SELECT COUNT(*) FROM vacancy_content_fetches WHERE candidate_id=? AND status='error' AND error_code='hh_content_invalid'", candidate) != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=? AND type='extract_vacancy'", candidate) != 0 {
			t.Fatal("invalid content reached extraction")
		}
	})
	t.Run("late snapshot rollback retries existing paid run", func(t *testing.T) {
		_, s, lease, candidate := hhDetailFixture(t)
		p := &hhDetailProvider{item: hhDetailItem(hhDetailBody())}
		fail := &hhDetailLateFailurePlanner{}
		executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER", Handoff: &parser.CandidateHandoff{Planner: fail}}
		if _, err := executor.Execute(s.ctx, lease); err == nil || !fail.seen || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 || s.count("SELECT COUNT(*) FROM vacancy_content_fetches WHERE candidate_id=?", candidate) != 0 {
			t.Fatal("snapshot/handoff rollback", err)
		}
		executor.Handoff = nil
		result, err := executor.Execute(s.ctx, lease)
		if err != nil || result.SnapshotID == "" || p.starts != 1 {
			t.Fatal("rollback repeated paid POST", result, err)
		}
	})
	for _, kind := range []string{"source", "roles", "candidate", "generation"} {
		t.Run("late "+kind+" fences public response", func(t *testing.T) {
			f, s, lease, candidate := hhDetailFixture(t)
			public := &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody()}}
			public.after = func() {
				switch kind {
				case "source":
					if _, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: 1}); err != nil {
						t.Error(err)
					}
				case "roles":
					s.exec("UPDATE vacancy_role_classification SET markdown='CI changed prompt',revision=revision+1 WHERE role_id=991303")
				case "candidate":
					s.exec("UPDATE vacancy_candidates SET vacancy_url='https://hh.ru/vacancy/999' WHERE id=?", candidate)
				case "generation":
					s.exec("UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000097' WHERE id=1")
					t.Cleanup(func() { s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.v.Generation) })
				}
			}
			_, err := (parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: public}).Execute(s.ctx, lease)
			if err == nil || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 {
				t.Fatal("stale full text attached", err)
			}
		})
	}
}

func TestMariaDBHHDetailKeyRotationDisposable(t *testing.T) {
	f, s, lease, candidate := hhDetailFixture(t)
	p := &hhDetailProvider{item: hhDetailItem(hhDetailBody())}
	p.afterRead = func() {
		if _, err := f.repo.Settings.SaveKey(f.ctx, f.v.Generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: 2}); err != nil {
			t.Error(err)
		}
	}
	executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER"}
	if _, err := executor.Execute(s.ctx, lease); !errors.Is(err, apifysettings.ErrChanged) || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 {
		t.Fatal("key rotation accepted stale detail", err)
	}
}

func TestMariaDBHHDetailAdmissionDisposable(t *testing.T) {
	t.Run("old owner before detail sends no request", func(t *testing.T) {
		_, s, lease, _ := hhDetailFixture(t)
		s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1")
		public := &hhPublicFixture{}
		_, err := (parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: public}).Execute(s.ctx, lease)
		if err == nil || public.calls != 0 {
			t.Fatal("stale owner admitted detail", err)
		}
	})
	for _, kind := range []string{"roles", "scheduler", "favorites", "owner"} {
		t.Run("public delay then "+kind+" change sends no paid request", func(t *testing.T) {
			_, s, lease, candidate := hhDetailFixture(t)
			before := s.count("SELECT COUNT(*) FROM apify_runs")
			p := &hhDetailProvider{item: hhDetailItem(hhDetailBody())}
			public := &hhPublicFixture{}
			public.after = func() {
				if kind == "roles" {
					s.exec("UPDATE vacancy_role_classification SET markdown='CI stale preflight',revision=revision+1 WHERE role_id=991303")
				} else if kind == "favorites" {
					s.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
				} else if kind == "owner" {
					s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1")
				} else {
					s.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false,"sourceIds":["hh"]}' WHERE id=1`)
				}
			}
			executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: public, Owner: "CI_OWNER"}
			_, err := executor.Execute(s.ctx, lease)
			if err == nil || p.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != before || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 {
				t.Fatal("stale preflight admitted paid Actor", err, p.starts)
			}
		})
	}
}

func TestMariaDBHHDetailAccountAdmissionDisposable(t *testing.T) {
	for _, kind := range []string{"roles", "scheduler", "favorites", "owner"} {
		t.Run(kind+" change during account refresh sends no paid request", func(t *testing.T) {
			_, s, lease, candidate := hhDetailFixture(t)
			before := s.count("SELECT COUNT(*) FROM apify_runs")
			p := &hhDetailProvider{item: hhDetailItem(hhDetailBody())}
			p.afterAccount = func() {
				switch kind {
				case "roles":
					s.exec("UPDATE vacancy_role_classification SET markdown='CI stale account preflight',revision=revision+1 WHERE role_id=991303")
				case "favorites":
					s.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
				case "owner":
					s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1")
				default:
					s.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false,"sourceIds":["hh"]}' WHERE id=1`)
				}
			}
			executor := parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: hhDetailRuntime(s, p), Public: &hhPublicFixture{}, Owner: "CI_OWNER"}
			_, err := executor.Execute(s.ctx, lease)
			if err == nil || p.accounts != 1 || p.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != before || s.count("SELECT COUNT(*) FROM vacancy_raw_snapshots WHERE candidate_id=?", candidate) != 0 {
				t.Fatal("stale account refresh admitted paid Actor", err, p.starts, p.accounts)
			}
		})
	}
}
