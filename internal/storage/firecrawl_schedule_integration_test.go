//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/firecrawl"
	"itmap/internal/parser"
)

func TestMariaDBFirecrawlScheduleDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	f.exec(`UPDATE firecrawl_settings SET enabled=1,key_encrypted='CI_SYNTHETIC',favorites_only=1,
		scheduler_json='{"enabled":true,"ownerName":"CI_OWNER","sourceIds":["companySite"],"intervalDays":1}' WHERE id=1`)
	f.exec(`INSERT INTO company_identity(id,company_name) VALUES
		(94801,'CI Firecrawl Due'),(94802,'CI Firecrawl Unsupported'),(94803,'CI Firecrawl Not Favorite')`)
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_source_parsing WHERE company_id IN (94801,94802,94803)")
		f.exec("DELETE FROM company_user_state WHERE user_name='CI_OWNER' AND company_name IN ('CI Firecrawl Due','CI Firecrawl Unsupported')")
		f.exec("DELETE FROM company_crm WHERE company_name IN ('CI Firecrawl Due','CI Firecrawl Unsupported')")
		f.exec("DELETE FROM company_identity WHERE id IN (94801,94802,94803)")
	})
	f.exec(`INSERT INTO company_crm(company_name) VALUES ('CI Firecrawl Due'),('CI Firecrawl Unsupported')`)
	f.exec(`INSERT INTO company_user_state(company_name,user_name,favorite) VALUES
		('CI Firecrawl Due','CI_OWNER',1),('CI Firecrawl Unsupported','CI_OWNER',1)`)
	f.exec(`INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled,auto_enabled,config_json) VALUES
		(94801,'companySite','https://careers.company.com/jobs',1,1,'{"adapter":"generic","html":false,"titleClass":"","detailPathPrefix":"","allowedHosts":[],"pagination":null,"useEnhancedProxy":false}'),
		(94802,'companySite','https://careers.company.com/jobs',1,1,'{"adapter":"selectel"}'),
		(94803,'companySite','https://careers.company.com/jobs',1,1,'{}')`)
	run := f.begin("CI_FIRECRAWL_SCHEDULE")
	for i := 0; i < 2; i++ {
		if err := f.repo.ScheduleCompanySites(f.ctx, run.Run.ID, "CI_OWNER"); err != nil {
			t.Fatal(err)
		}
	}
	if all, due := f.count("SELECT COUNT(*) FROM scheduler_jobs"), f.count(`SELECT COUNT(*) FROM scheduler_jobs
		WHERE source_id='companySite' AND type='scan_source' AND company_id=94801 AND JSON_EXTRACT(payload,'$.firecrawlVersion')=1`); all != 1 || due != 1 {
		t.Fatalf("expected only one eligible scheduled scan, total=%d eligible=%d", all, due)
	}
	if f.count("SELECT COUNT(*) FROM vacancy_source_parsing WHERE company_id=94801 AND next_run_at>?", schedulerClock(t, f.db)) != 1 {
		t.Fatal("scan admission did not advance its schedule")
	}
	f.finish(run)
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order) VALUES(94801,'CI_FIRECRAWL_ROLE','CI Engineering',0)")
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_role_classification WHERE role_id=94801")
		f.exec("DELETE FROM vacancy_roles WHERE id=94801")
	})
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(94801,'CI Engineering rules',1,1)")
	t.Cleanup(func() {
		f.exec("DELETE FROM parser_candidate_intakes WHERE source_id='companySite' AND candidate_token IN (SELECT creation_token FROM vacancy_candidates WHERE company_id=94801)")
		f.exec("DELETE FROM vacancy_candidate_identities WHERE candidate_id IN (SELECT id FROM vacancy_candidates WHERE company_id=94801)")
		f.exec("DELETE FROM vacancy_candidates WHERE company_id=94801")
	})
	schedulerSeedJob(f, "CI_FIRECRAWL_LEGACY", nil)
	f.exec("UPDATE scheduler_jobs SET type='scan_source',source_id='companySite',priority=100 WHERE id='CI_FIRECRAWL_LEGACY'")
	schedulerSeedJob(f, "CI_FIRECRAWL_HH", nil)
	f.exec("UPDATE scheduler_jobs SET type='scan_source',source_id='hh',priority=100 WHERE id='CI_FIRECRAWL_HH'")
	provider := &syntheticFirecrawlListing{t: t}
	worker := parser.Worker{Runs: f.repo, Settings: apifysettings.Repository{DB: f.db}, Firecrawl: provider}
	result, err := worker.Cycle(f.ctx, parser.WorkerOptions{
		Run:          parser.RunOptions{RequestID: "CI_FIRECRAWL_PROFILE", Trigger: "automatic"},
		QueueProfile: "companySite", MaxJobs: 3,
	})
	if err != nil || result.Run.Status != "completed" || result.Processed != 1 || provider.calls != 1 {
		t.Fatalf("company worker cycle: status=%s processed=%d calls=%d err=%v", result.Run.Status, result.Processed, provider.calls, err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id IN ('CI_FIRECRAWL_LEGACY','CI_FIRECRAWL_HH') AND status='pending'") != 2 {
		t.Fatal("company worker touched another queue")
	}
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=94801 AND source_id='companySite' AND title_raw='Senior Go Engineer'") != 1 ||
		f.count("SELECT COUNT(*) FROM vacancy_source_parsing WHERE company_id=94801 AND last_result='success'") != 1 {
		t.Fatal("company worker did not save the candidate and source receipt")
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE source_id='companySite' AND type='classify_title' AND JSON_EXTRACT(payload,'$.firecrawlVersion')=1") != 1 {
		t.Fatal("company worker did not queue the next stage")
	}

}

type syntheticFirecrawlListing struct {
	t     *testing.T
	calls int
}

func (f *syntheticFirecrawlListing) Execute(_ context.Context, lease *parser.JobLease, step parser.FirecrawlScrapeStep) (firecrawl.ScrapeResult, error) {
	f.calls++
	if lease.Job().Type != "scan_source" || step.URL != "https://careers.company.com/jobs" {
		f.t.Errorf("unexpected Firecrawl step: %s", step.ID)
	}
	return firecrawl.ScrapeResult{FinalURL: step.URL, TargetStatus: 200, Markdown: "[Senior Go Engineer](/jobs/42)"}, nil
}
