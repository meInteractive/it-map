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
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type hhScanProvider struct {
	mu            sync.Mutex
	starts        int
	accounts      int
	afterAccount  func()
	rows          [][]json.RawMessage
	afterRead     func()
	modified      bool
	pollSuccess   bool
	requireMoscow bool
}

func (p *hhScanProvider) Account(ctx context.Context) (apify.BillingAccount, error) {
	p.mu.Lock()
	p.accounts++
	hook := p.afterAccount
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	return (&runtimeProvider{}).Account(ctx)
}
func (p *hhScanProvider) Start(_ context.Context, source string, o apify.HHOptions) (apify.ActorRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if source != "https://hh.ru/employer/456" {
		return apify.ActorRun{}, fmt.Errorf("wrong source")
	}
	if p.requireMoscow {
		input, err := apify.HHInput(source, o)
		if err != nil {
			return apify.ActorRun{}, err
		}
		u, err := url.Parse(input.StartURL)
		if err != nil || !o.MoscowFilter || !o.DateFilterApplied || o.DateFrom == nil || o.DateTo == nil || u.Query().Get("date_from") != *o.DateFrom || u.Query().Get("date_to") != *o.DateTo || u.Query().Get("area") != "1" || u.Query().Get("employer_id") != "456" || u.Query().Has("text") || !reflect.DeepEqual(u.Query()["professional_role"], []string{"36", "107"}) {
			return apify.ActorRun{}, fmt.Errorf("Moscow scope missing")
		}
	}
	p.starts++
	return hhScanRun(o.Page), nil
}
func (p *hhScanProvider) Run(context.Context, string) (apify.ActorRun, error) {
	if p.pollSuccess {
		return hhScanRun(0), nil
	}
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
	posted := time.Now().In(time.FixedZone("MSK", 3*60*60)).AddDate(0, 0, -1).Format(time.RFC3339)
	return json.RawMessage(fmt.Sprintf(`{"title":%q,"url":"https://hh.ru/vacancy/%d?utm_source=ci","vacancy_id":%d,"employer_id":456,"location":"Москва","date_posted":%q}`, title, id, id, posted))
}
func hhScanFixture(t *testing.T, p *hhScanProvider) (apifyRuntimeFixture, *schedulerFixture, *parser.JobLease, parser.HHListingExecutor) {
	t.Helper()
	f := openApifyRuntimeFixture(t)
	s := billingFixture(t)
	s.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["hh"],"ownerName":"CI_OWNER","dayBudgetUsd":5,"monthBudgetUsd":5,"hhProfessionalRoles":{"991303":{"revision":1,"roleIds":["36","107"]}}}' WHERE id=1`)
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
	t.Run("missing or unmapped roles never start an Actor", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
		f, s, lease, listing := hhScanFixture(t, p)
		s.exec("UPDATE vacancy_role_classification SET parsing_enabled=0 WHERE role_id=991303")
		if _, err := listing.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHRoleFilter) || p.starts != 0 || p.accounts != 0 {
			t.Fatal("missing role admitted", err, p.starts, p.accounts)
		}
		s.exec("UPDATE vacancy_role_classification SET parsing_enabled=1 WHERE role_id=991303")
		s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_REMOVE(scheduler_json,'$.hhProfessionalRoles') WHERE id=1`)
		if _, err := listing.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHRoleFilter) || p.starts != 0 || p.accounts != 0 {
			t.Fatal("unknown role admitted", err, p.starts, p.accounts)
		}
		if s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
			t.Fatal("rejected scope reserved an Actor")
		}
	})
	t.Run("role change during account check denies Actor", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
		f, s, lease, listing := hhScanFixture(t, p)
		changed := false
		p.afterAccount = func() {
			s.exec("UPDATE vacancy_role_classification SET parsing_enabled=0 WHERE role_id=991303")
			changed = true
		}
		if _, err := listing.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHRoleFilter) || !changed || p.starts != 0 {
			t.Fatal("changed roles admitted", err, changed, p.starts)
		}
		if s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
			t.Fatal("changed scope reserved an Actor")
		}
	})
	t.Run("non-Moscow and unknown locations never reach classification", func(t *testing.T) {
		moscow := json.RawMessage(strings.Replace(string(hhScanItem(123, "CI Moscow")), `"location":"Москва"`, `"location":"Москве"`, 1))
		other := json.RawMessage(strings.Replace(string(hhScanItem(124, "CI SPB")), `"location":"Москва"`, `"location":"Санкт-Петербург"`, 1))
		unknown := json.RawMessage(strings.Replace(string(hhScanItem(125, "CI unknown")), `,"location":"Москва"`, ``, 1))
		p := &hhScanProvider{requireMoscow: true, rows: [][]json.RawMessage{{moscow, other, unknown}}}
		f, s, lease, e := hhScanFixture(t, p)
		progress, err := e.Execute(f.ctx, lease)
		if err != nil || progress.Candidates != 1 || progress.OutsideMoscow != 1 || progress.Quarantined != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title'") != 1 {
			t.Fatal("location guard", progress, err)
		}
	})
	t.Run("legacy actor continues but a new unfiltered actor is forbidden", func(t *testing.T) {
		items := make([]json.RawMessage, 11)
		for i := range items {
			items[i] = hhScanItem(123+i, "CI_TITLE")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{items}}
		f, s, lease, listing := hhScanFixture(t, p)
		first, err := listing.Execute(f.ctx, lease)
		if err != nil || first.Done || p.starts != 1 {
			t.Fatal("initial scope", first, err)
		}
		var raw []byte
		if err = s.db.QueryRowContext(f.ctx, "SELECT result_json FROM vacancy_discovery_runs WHERE id='go399:CI_HH_SCAN'").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var cursor struct {
			Operation                    apifysettings.OperationVersion
			Page                         int64
			TitleQuery, DateFrom, DateTo *string
			ProfessionalRoles            []string
			DateFilterApplied            bool
		}
		if json.Unmarshal(raw, &cursor) != nil {
			t.Fatal("cursor")
		}
		s.exec(`UPDATE vacancy_discovery_runs SET result_json=JSON_REMOVE(result_json,'$.MoscowFilter') WHERE id='go399:CI_HH_SCAN'`)
		if _, err = listing.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHListingChanged) || p.starts != 1 {
			t.Fatal("unfiltered Actor was not blocked", err, p.starts)
		}
		runtime := listing.Runtime.(apifyruns.Service)
		order := "publication_time"
		opts := apify.HHOptions{Page: cursor.Page, OrderBy: &order, TitleQuery: cursor.TitleQuery, ProfessionalRoles: cursor.ProfessionalRoles, DateFrom: cursor.DateFrom, DateTo: cursor.DateTo, DateFilterApplied: cursor.DateFilterApplied}
		if _, err = runtime.Execute(f.ctx, cursor.Operation, fmt.Sprintf("listing:go399:CI_HH_SCAN:%d", cursor.Page), opts); err != nil || p.starts != 2 {
			t.Fatal("seed existing legacy actor", err, p.starts)
		}
		next, err := listing.Execute(f.ctx, lease)
		if err != nil || !next.Done || p.starts != 2 {
			t.Fatal("legacy dataset not continued", next, err, p.starts)
		}
	})
	t.Run("paid title-filter actor is read using its original input", func(t *testing.T) {
		items := make([]json.RawMessage, 11)
		for i := range items {
			items[i] = hhScanItem(9000+i, "CI old paid")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{items}}
		f, s, lease, listing := hhScanFixture(t, p)
		if _, err := listing.Execute(f.ctx, lease); err != nil || p.starts != 1 {
			t.Fatal("initial setup", err)
		}
		var raw []byte
		if err := s.db.QueryRowContext(f.ctx, "SELECT result_json FROM vacancy_discovery_runs WHERE id='go399:CI_HH_SCAN'").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var cursor struct {
			Operation         apifysettings.OperationVersion
			Page              int64
			DateFrom, DateTo  *string
			DateFilterApplied bool
		}
		if json.Unmarshal(raw, &cursor) != nil {
			t.Fatal("cursor")
		}
		query, order := "NAME:(manager*)", "publication_time"
		opts := apify.HHOptions{Page: cursor.Page, OrderBy: &order, TitleQuery: &query, DateFrom: cursor.DateFrom, DateTo: cursor.DateTo, DateFilterApplied: cursor.DateFilterApplied, MoscowFilter: true}
		runtime := listing.Runtime.(apifyruns.Service)
		if _, err := runtime.Execute(f.ctx, cursor.Operation, fmt.Sprintf("listing:go399:CI_HH_SCAN:%d", cursor.Page), opts); err != nil || p.starts != 2 {
			t.Fatal("seed old paid actor", err)
		}
		s.exec(`UPDATE vacancy_discovery_runs SET result_json=JSON_SET(JSON_REMOVE(result_json,'$.ProfessionalRoles'),'$.TitleQuery',?) WHERE id='go399:CI_HH_SCAN'`, query)
		last, err := listing.Execute(f.ctx, lease)
		if err != nil || !last.Done || last.Offset != 11 || p.starts != 2 {
			t.Fatal("old dataset was not resumed without new Actor", last, err, p.starts)
		}
	})
	t.Run("keeps out-of-range and missing dates from filtered Actor", func(t *testing.T) {
		moscow := time.FixedZone("MSK", 3*60*60)
		old := time.Now().In(moscow).AddDate(0, 0, -8).Format(time.RFC3339)
		today := time.Now().In(moscow).Format(time.RFC3339)
		outside := json.RawMessage(fmt.Sprintf(`{"title":"CI old","url":"https://hh.ru/vacancy/124","vacancy_id":124,"employer_id":456,"location":"Москве","date_posted":%q}`, old))
		missing := json.RawMessage(`{"title":"CI undated","url":"https://hh.ru/vacancy/125","vacancy_id":125,"employer_id":456,"location":"Москва"}`)
		current := json.RawMessage(fmt.Sprintf(`{"title":"CI today","url":"https://hh.ru/vacancy/126","vacancy_id":126,"employer_id":456,"location":"Москва","date_posted":%q}`, today))
		malformed := json.RawMessage(`{"title":"CI malformed date","url":"https://hh.ru/vacancy/127","vacancy_id":127,"employer_id":456,"location":"Москва","date_posted":"not-a-date"}`)
		p := &hhScanProvider{requireMoscow: true, rows: [][]json.RawMessage{{hhScanItem(123, "CI recent"), outside, missing, current, malformed}}}
		f, s, lease, e := hhScanFixture(t, p)
		progress, err := e.Execute(f.ctx, lease)
		if err != nil || !progress.Done || progress.Candidates != 5 || progress.Created != 5 || progress.Quarantined != 0 || progress.OutsideDateWindow != 2 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 5 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title'") != 5 || p.starts != 1 {
			t.Fatal("Actor results were filtered by publication date or Actor was repeated", progress, err)
		}
		if s.count("SELECT COUNT(*) FROM vacancy_discovery_runs WHERE company_id=991302 AND JSON_EXTRACT(result_json,'$.LineageVersion')=1 AND JSON_LENGTH(JSON_EXTRACT(result_json,'$.CandidateIDs'))=5 AND JSON_EXTRACT(result_json,'$.OutsideDateWindow')=2") != 1 {
			t.Fatal("accepted records or date diagnostics were not saved in cursor")
		}
	})
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
		if s.count("SELECT COUNT(*) FROM vacancy_discovery_runs WHERE company_id=991302 AND JSON_LENGTH(JSON_EXTRACT(result_json,'$.CandidateIDs'))=8") != 1 {
			t.Fatal("duplicate candidate changed scan lineage")
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
	t.Run("manual control resumes one saved job and replays its receipt", func(t *testing.T) {
		rows := make([]json.RawMessage, 11)
		for i := range rows {
			rows[i] = hhScanItem(8400+i, "CI manual resume")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, oldLease, executor := hhScanFixture(t, p)
		var oldRun string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&oldRun); err != nil {
			t.Fatal(err)
		}
		if err := oldLease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
			t.Fatal(err)
		}
		if err := s.repo.Finish(s.ctx, oldRun, "completed"); err != nil {
			t.Fatal(err)
		}
		const jobID = "12345678-1234-4234-8234-123456789abd"
		s.exec("UPDATE scheduler_jobs SET id=? WHERE id='CI_HH_SCAN'", jobID)
		firstRun := s.begin("CI resume first")
		lease := schedulerRequireLease(t, s, firstRun, jobID)
		first, err := executor.Execute(f.ctx, lease)
		if err != nil || first.Offset != 10 {
			t.Fatal("first slice", first, err)
		}
		if err = lease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
			t.Fatal(err)
		}
		s.finish(firstRun)
		paused, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: f.v.SourceRevision})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: true, Revision: paused.Revision}); err != nil {
			t.Fatal(err)
		}
		control := parser.NewCycleControlWithFactory(s.db, func(r parser.RunRepository) (parser.Worker, error) {
			return parser.Worker{Runs: r, Settings: executor.Settings, ListingRuntime: executor.Runtime}, nil
		})
		resume, err := control.PrepareResume(s.ctx)
		if err != nil {
			t.Fatal(err)
		}
		const requestID = "12345678-1234-4234-8234-123456789abc"
		if _, err = resume(s.ctx, requestID, jobID); err != nil || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 11 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending'") != 11 {
			t.Fatal("manual resume", err)
		}
		replayed, err := resume(s.ctx, requestID, jobID)
		if err != nil || !strings.Contains(string(replayed), `"reused":true`) || p.starts != 1 {
			t.Fatal("resume receipt replay", err)
		}
	})
	t.Run("explicit resume rejects rotated token without changing cursor", func(t *testing.T) {
		rows := make([]json.RawMessage, 11)
		for i := range rows {
			rows[i] = hhScanItem(8300+i, "CI rotated token")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, executor := hhScanFixture(t, p)
		if _, err := executor.Execute(f.ctx, lease); err != nil {
			t.Fatal("first slice", err)
		}
		view, err := f.repo.Settings.Read(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.repo.Settings.SaveKey(f.ctx, f.v.Generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: view.Revision}); err != nil {
			t.Fatal("rotate token", err)
		}
		executor.ResumePrevious = true
		if _, err = executor.Execute(f.ctx, lease); !errors.Is(err, apifyruns.ErrDatasetChanged) || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 10 || s.count("SELECT COUNT(*) FROM vacancy_discovery_runs WHERE company_id=991302 AND settings_revision=2") != 1 {
			t.Fatal("rotated token admitted or cursor changed", err)
		}
	})
	t.Run("explicit resume rejects changed search without another Actor", func(t *testing.T) {
		rows := make([]json.RawMessage, 11)
		for i := range rows {
			rows[i] = hhScanItem(8200+i, "CI changed search")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, executor := hhScanFixture(t, p)
		if _, err := executor.Execute(f.ctx, lease); err != nil {
			t.Fatal("first slice", err)
		}
		paused, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: f.v.SourceRevision})
		if err != nil {
			t.Fatal("pause source", err)
		}
		if _, err = f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: true, Revision: paused.Revision}); err != nil {
			t.Fatal("enable source", err)
		}
		s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.hhProfessionalRoles.991303.roleIds',JSON_ARRAY('73')) WHERE id=1`)
		executor.ResumePrevious = true
		if _, err = executor.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHListingChanged) || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 10 {
			t.Fatal("changed search admitted", err)
		}
	})
	t.Run("explicit resume reads terminal dataset after source pause", func(t *testing.T) {
		rows := make([]json.RawMessage, 11)
		for i := range rows {
			rows[i] = hhScanItem(8100+i, "CI resume")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, executor := hhScanFixture(t, p)
		first, err := executor.Execute(f.ctx, lease)
		if err != nil || first.Offset != 10 || p.starts != 1 {
			t.Fatal("first slice", first, err)
		}
		paused, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: f.v.SourceRevision})
		if err != nil {
			t.Fatal("pause source", err)
		}
		if _, err = f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: true, Revision: paused.Revision}); err != nil {
			t.Fatal("enable source", err)
		}
		executor.ResumePrevious = true
		last, err := executor.Execute(f.ctx, lease)
		if err != nil || !last.Done || last.Offset != 11 || p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 11 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
			t.Fatal("resume failed or created a new Actor", last, err)
		}
	})
	t.Run("previous terminal dataset needs current authorization and same token", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(7999, "CI prior dataset")}}}
		f := openApifyRuntimeFixture(t)
		service := apifyruns.Service{Repository: f.repo, NewClient: func(string) (apifyruns.Provider, error) { return p, nil }}
		original, err := service.Execute(f.ctx, f.v, "CI previous", apify.HHOptions{})
		if err != nil || original.State != "succeeded" || original.ChargedUSD == nil {
			t.Fatal("terminal operation", err)
		}
		paused, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: f.v.SourceRevision})
		if err != nil {
			t.Fatal("pause", err)
		}
		active, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: true, Revision: paused.Revision})
		if err != nil {
			t.Fatal("resume", err)
		}
		current := f.v
		current.SourceRevision = active.Revision
		slice, err := service.ReadPreviousDataset(f.ctx, f.v, current, "CI previous", apify.HHOptions{}, 0, original.RunID, original.DatasetID)
		if err != nil || len(slice.Items) != 1 || p.starts != 1 {
			t.Fatal("previous dataset not reused", err)
		}
		if _, err = service.ReadPreviousDataset(f.ctx, f.v, current, "CI previous", apify.HHOptions{}, 0, "wrong", original.DatasetID); !errors.Is(err, apifyruns.ErrDatasetChanged) || p.starts != 1 {
			t.Fatal("mismatched run admitted", err)
		}
		view, err := f.repo.Settings.Read(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := f.repo.Settings.SaveKey(f.ctx, f.v.Generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: view.Revision})
		if err != nil {
			t.Fatal("rotate token", err)
		}
		current.SettingsRevision = updated.Revision
		if _, err = service.ReadPreviousDataset(f.ctx, f.v, current, "CI previous", apify.HHOptions{}, 0, original.RunID, original.DatasetID); !errors.Is(err, apifysettings.ErrChanged) || p.starts != 1 {
			t.Fatal("rotated token admitted", err)
		}
	})
	t.Run("source pause fences the old cursor without another Actor", func(t *testing.T) {
		rows := make([]json.RawMessage, 11)
		for i := range rows {
			rows[i] = hhScanItem(8000+i, "CI pause")
		}
		p := &hhScanProvider{rows: [][]json.RawMessage{rows}}
		f, s, lease, e := hhScanFixture(t, p)
		first, err := e.Execute(f.ctx, lease)
		if err != nil || first.Offset != 10 || p.starts != 1 {
			t.Fatal("first slice", first, err)
		}
		paused, err := f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: false, Revision: f.v.SourceRevision})
		if err != nil {
			t.Fatal("pause source", err)
		}
		if _, err = f.repo.Settings.SaveHHSource(f.ctx, f.v.Generation, 991302, apifysettings.HHSourceInput{SourceURL: f.v.SourceURL, Enabled: true, Revision: paused.Revision}); err != nil {
			t.Fatal("re-enable source", err)
		}
		if _, err = e.Execute(f.ctx, lease); !errors.Is(err, parser.ErrHHListingChanged) {
			t.Fatal("stale cursor should be fenced", err)
		}
		if p.starts != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 10 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
			t.Fatal("pause changed saved data or started another Actor")
		}
	})
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
		if s.count("SELECT COUNT(*) FROM vacancy_discovery_runs WHERE company_id=991302 AND JSON_EXTRACT(result_json,'$.CandidateIDs') IS NOT NULL") != 0 {
			t.Fatal("rolled back candidate survived in scan lineage")
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

func TestMariaDBHHListingAdmissionDisposable(t *testing.T) {
	t.Run("old owner before listing spends nothing", func(t *testing.T) {
		p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(9000, "CI stale listing")}}}
		f, s, lease, e := hhScanFixture(t, p)
		s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1")
		if _, err := e.Execute(f.ctx, lease); err == nil || p.accounts != 0 || p.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
			t.Fatal("stale owner admitted listing", err)
		}
	})
	for _, kind := range []string{"scheduler", "favorites", "owner"} {
		t.Run(kind+" change during account refresh blocks listing", func(t *testing.T) {
			p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(9000, "CI stale listing")}}}
			f, s, lease, e := hhScanFixture(t, p)
			p.afterAccount = func() {
				if kind == "favorites" {
					s.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
				} else if kind == "owner" {
					s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1")
				} else {
					s.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false,"sourceIds":["hh"]}' WHERE id=1`)
				}
			}
			if _, err := e.Execute(f.ctx, lease); err == nil || p.accounts != 1 || p.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 0 {
				t.Fatal("stale listing admitted Actor", err, p.starts, p.accounts)
			}
		})
	}
}

func TestMariaDBHHListingQueueGuardDisposable(t *testing.T) {
	p := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(990123, "CI verified")}}}
	f, s, lease, listing := hhScanFixture(t, p)
	progress, err := listing.Execute(f.ctx, lease)
	if err != nil || progress.Candidates != 1 {
		t.Fatal("verified listing", progress, err)
	}
	if err = lease.Finish(f.ctx, parser.FinishOptions{Status: "completed"}); err != nil {
		t.Fatal("finish scan", err)
	}
	var run string
	if err = s.db.QueryRowContext(f.ctx, "SELECT id FROM scheduler_runs WHERE status='running' LIMIT 1").Scan(&run); err != nil {
		t.Fatal("running cycle", err)
	}
	var verified int64
	if err = s.db.QueryRowContext(f.ctx, "SELECT id FROM vacancy_candidates WHERE company_id=991302 LIMIT 1").Scan(&verified); err != nil {
		t.Fatal("verified candidate", err)
	}
	s.exec(`INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(991399,'CI_OLD_HH',991302,'CI old title',?,'https://hh.ru/vacancy/990124','hh','site','2026-01-01','2026-01-01','2026-01-01','2026-01-01')`, strings.Repeat("a", 64))
	s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_OLD_HH_JOB',?,'CI_OLD_HH_JOB','classify_title',991302,991399,'hh','{}',100,0,0,0)`, s.repo.Generation)
	claimed, err := s.repo.ClaimJob(f.ctx, run, parser.ClaimOptions{Types: []string{"classify_title"}, VerifiedHHOnly: true})
	if err != nil || claimed == nil || claimed.Job().CandidateID == nil || *claimed.Job().CandidateID != verified {
		t.Fatal("verified job not selected over old job", err)
	}
	if err = claimed.Finish(f.ctx, parser.FinishOptions{Status: "completed"}); err != nil {
		t.Fatal("finish candidate", err)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_OLD_HH_JOB' AND status='pending'") != 1 {
		t.Fatal("old job changed")
	}
	s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_OLD_SCAN',?,'CI_OLD_SCAN','scan_source',991302,'hh','{}',100,0,0,0)`, s.repo.Generation)
	s.exec(`INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES('go399:CI_OLD_SCAN','CI_OWNER',?,991302,'hh',?,?,?, '{"Version":1}',0,0)`, s.repo.Generation, f.v.SourceURL, f.v.SourceRevision, f.v.SettingsRevision)
	claimed, err = s.repo.ClaimJob(f.ctx, run, parser.ClaimOptions{Types: []string{"scan_source"}, VerifiedHHOnly: true})
	if err != nil || claimed != nil {
		t.Fatal("old scan admitted to global cycle", err)
	}
	s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_VERIFIED_AGAIN',?,'CI_VERIFIED_AGAIN','classify_title',991302,?,'hh','{}',100,0,0,0)`, s.repo.Generation, verified)
	s.exec("UPDATE vacancy_source_parsing SET revision=revision+1 WHERE company_id=991302 AND source_id='hh'")
	claimed, err = s.repo.ClaimJob(f.ctx, run, parser.ClaimOptions{Types: []string{"classify_title"}, VerifiedHHOnly: true})
	if err != nil || claimed != nil {
		t.Fatal("changed source admitted", err)
	}
}
