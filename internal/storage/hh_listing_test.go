//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"itmap/internal/parser"
	"sync"
	"testing"
)

type hhScanProvider struct {
	mu        sync.Mutex
	starts    int
	rows      [][]json.RawMessage
	afterRead func()
	modified  bool
}

func (p *hhScanProvider) Account(ctx context.Context) (apify.BillingAccount, error) {
	return (&runtimeProvider{}).Account(ctx)
}
func (p *hhScanProvider) Start(_ context.Context, source string, o apify.HHOptions) (apify.ActorRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if source != "https://hh.ru/employer/456" {
		return apify.ActorRun{}, fmt.Errorf("wrong source")
	}
	p.starts++
	return hhScanRun(o.Page), nil
}
func (p *hhScanProvider) Run(context.Context, string) (apify.ActorRun, error) {
	return apify.ActorRun{}, fmt.Errorf("terminal run should not need poll")
}
func hhScanRun(page int64) apify.ActorRun {
	id := fmt.Sprintf("HHRun%d", page)
	data := fmt.Sprintf("HHData%d", page)
	return apify.ActorRun{ID: id, DefaultDatasetID: data, Status: "SUCCEEDED", Raw: json.RawMessage(fmt.Sprintf(`{"id":%q,"defaultDatasetId":%q,"status":"SUCCEEDED","userId":"CIRuntime","usageTotalUsd":0.02}`, id, data))}
}
func (p *hhScanProvider) DatasetInfo(_ context.Context, id string) (apify.DatasetMetadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var page int
	if _, e := fmt.Sscanf(id, "HHData%d", &page); e != nil || page >= len(p.rows) {
		return apify.DatasetMetadata{}, fmt.Errorf("unknown dataset")
	}
	modified := "2020-01-01T00:00:00Z"
	if p.modified {
		modified = "2020-01-02T00:00:00Z"
	}
	return apify.DatasetMetadata{ItemCount: int64(len(p.rows[page])), ModifiedAt: modified}, nil
}
func (p *hhScanProvider) DatasetItems(_ context.Context, id string, offset int64, clean bool) ([]json.RawMessage, error) {
	p.mu.Lock()
	var page int
	if _, err := fmt.Sscanf(id, "HHData%d", &page); err != nil || page >= len(p.rows) || clean {
		p.mu.Unlock()
		return nil, fmt.Errorf("unknown raw dataset")
	}
	end := offset + 10
	if end > int64(len(p.rows[page])) {
		end = int64(len(p.rows[page]))
	}
	if offset > end {
		p.mu.Unlock()
		return nil, fmt.Errorf("bad offset")
	}
	items := append([]json.RawMessage(nil), p.rows[page][offset:end]...)
	hook := p.afterRead
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	return items, nil
}
func hhScanItem(id int, title string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"title":%q,"url":"https://hh.ru/vacancy/%d?utm_source=ci","vacancy_id":%d,"employer_id":456}`, title, id, id))
}
func hhScanFixture(t *testing.T, p *hhScanProvider) (apifyRuntimeFixture, *schedulerFixture, *parser.JobLease, parser.HHListingExecutor) {
	t.Helper()
	f := openApifyRuntimeFixture(t)
	s := billingFixture(t)
	s.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["hh"],"dayBudgetUsd":5,"monthBudgetUsd":5}' WHERE id=1`)
	s.exec(`INSERT INTO vacancy_roles(id,name,stable_key,sort_order) VALUES(991303,'CI HH Root','root-it-manager',1)`)
	s.exec(`INSERT INTO vacancy_role_classification(role_id,markdown,parsing_enabled) VALUES(991303,'CI HH classification',1)`)
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM scheduler_jobs", "DELETE FROM parser_candidate_intakes WHERE candidate_token IN (SELECT creation_token FROM vacancy_candidates WHERE company_id=991302)", "DELETE FROM vacancy_candidates WHERE company_id=991302", "DELETE FROM vacancy_roles WHERE id=991303", "DELETE FROM vacancy_discovery_runs WHERE company_id=991302"} {
			if _, e := s.db.ExecContext(context.Background(), q); e != nil {
				t.Error("HH cleanup", e)
			}
		}
	})
	run := s.begin("CI HH scan")
	s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_HH_SCAN',?,'CI_HH_SCAN','scan_source',991302,'hh','{"companyId":999,"sourceUrl":"https://ci.invalid"}',90,0,0,0)`, s.repo.Generation)
	lease := schedulerRequireLease(t, s, run, "CI_HH_SCAN")
	runtime := apifyruns.Service{Repository: f.repo, NewClient: func(string) (apifyruns.Provider, error) { return p, nil }}
	return f, s, lease, parser.HHListingExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Runtime: runtime, Owner: "CI_OWNER"}
}
func TestMariaDBHHListingDisposable(t *testing.T) {
	t.Run("raw pagination quarantine duplicate URL title revision and restart", func(t *testing.T) {
		rows := []json.RawMessage{}
		for i := 0; i < 8; i++ {
			rows = append(rows, hhScanItem(100+i, "CI title"))
		}
		rows = append(rows, hhScanItem(100, "CI title"), json.RawMessage(`{}`), hhScanItem(100, "CI changed title"))
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, e := hhScanFixture(t, p)
		first, err := e.Execute(f.ctx, lease)
		if err != nil || first.Offset != 10 || first.Done || first.Quarantined != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 8 {
			t.Fatal("first slice not persisted", first, err)
		}
		resumed := e
		last, err := resumed.Execute(f.ctx, lease)
		if err != nil || !last.Done || last.Offset != 11 || last.PartialCode != "apify_partial_data" || p.starts != 1 {
			t.Fatal("restart failed", last, err)
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302 AND vacancy_url='https://hh.ru/vacancy/100' AND title_raw='CI changed title' AND title_revision=2") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title'") != 9 || s.count("SELECT COUNT(*) FROM parser_candidate_intakes WHERE parser='apify'") != 8 {
			t.Fatal("candidate/classification/provenance mismatch")
		}
		if _, err = resumed.Execute(f.ctx, lease); err != nil || p.starts != 1 {
			t.Fatal("completed scan replay started Actor", err)
		}
	})
	t.Run("two HH pages preserve exact page and run identity", func(t *testing.T) {
		first := []json.RawMessage{}
		for i := 0; i < 100; i++ {
			first = append(first, hhScanItem(1000+i, "CI HH title"))
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{first, {hhScanItem(2000, "CI HH final")}}}
		f, s, lease, e := hhScanFixture(t, p)
		var progress parser.HHListingProgress
		for i := 0; i < 12; i++ {
			var err error
			progress, err = e.Execute(f.ctx, lease)
			if err != nil {
				t.Fatal("page slice", i, err)
			}
		}
		if !progress.Done || progress.Page != 1 || p.starts != 2 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 101 {
			t.Fatal("HH pagination lost data or restarted", progress, p.starts)
		}
	})
}

type hhLateFailurePlanner struct{ seen bool }

func (p *hhLateFailurePlanner) Next(ctx context.Context, tx *sql.Tx, j parser.Job) (*parser.CandidateStagePlan, error) {
	var n int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302").Scan(&n) == nil && n > 0 {
		p.seen = true
	}
	return nil, errors.New("CI late handoff failure")
}
func TestMariaDBHHListingRecoveryDisposable(t *testing.T) {
	t.Run("empty stable dataset completes", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{}}}
		f, _, lease, e := hhScanFixture(t, p)
		progress, err := e.Execute(f.ctx, lease)
		if err != nil || !progress.Done || progress.Created != 0 || p.starts != 1 {
			t.Fatal("empty dataset", progress, err)
		}
	})
	t.Run("concurrent slices cannot duplicate or regress cursor", func(t *testing.T) {
		rows := []json.RawMessage{}
		for i := 0; i < 11; i++ {
			rows = append(rows, hhScanItem(3000+i, "CI race"))
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, e := hhScanFixture(t, p)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := e.Execute(f.ctx, lease); err != nil && !errors.Is(err, apifyruns.ErrUncertain) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		progress, err := e.Execute(f.ctx, lease)
		if err != nil || !progress.Done || progress.Offset != 11 || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 11 {
			t.Fatal("concurrent cursor/replay", progress, err)
		}
	})
	t.Run("late SQL rollback retains Actor identity for retry", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(4000, "CI rollback")}}}
		f, s, lease, e := hhScanFixture(t, p)
		fail := &hhLateFailurePlanner{}
		e.Handoff = &parser.CandidateHandoff{Planner: fail}
		if _, err := e.Execute(f.ctx, lease); err == nil || !fail.seen || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 0 || s.count("SELECT COUNT(*) FROM parser_candidate_intakes WHERE parser='apify'") != 0 {
			t.Fatal("late handoff failure did not roll back", err)
		}
		e.Handoff = nil
		progress, err := e.Execute(f.ctx, lease)
		if err != nil || !progress.Done || progress.Created != 1 || p.starts != 1 {
			t.Fatal("rollback retry started second Actor", progress, err)
		}
	})
	for _, kind := range []string{"dataset", "source", "favorites"} {
		t.Run("late "+kind+" rejects entire slice", func(t *testing.T) {
			p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(5000, "CI stale")}}}
			f, s, lease, e := hhScanFixture(t, p)
			p.afterRead = func() {
				switch kind {
				case "dataset":
					p.mu.Lock()
					p.modified = true
					p.mu.Unlock()
				case "source":
					if _, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: 1}); err != nil {
						t.Error(err)
					}
				case "favorites":
					s.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
				}
			}
			if _, err := e.Execute(f.ctx, lease); err == nil || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title'") != 0 {
				t.Fatal("late change accepted", err)
			}
		})
	}
}
