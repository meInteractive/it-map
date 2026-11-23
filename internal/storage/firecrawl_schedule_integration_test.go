//go:build mariadb && schedulerwrites

package storage

import "testing"

func TestMariaDBFirecrawlScheduleDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	f.exec(`UPDATE firecrawl_settings SET enabled=1,key_encrypted='CI_SYNTHETIC',favorites_only=1,
		scheduler_json='{"enabled":true,"ownerName":"CI_OWNER","sourceIds":["companySite"],"intervalDays":1}' WHERE id=1`)
	f.exec(`INSERT INTO company_identity(id,company_name) VALUES
		(94801,'CI Firecrawl Due'),(94802,'CI Firecrawl Unsupported'),(94803,'CI Firecrawl Not Favorite')`)
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_source_parsing WHERE company_id IN (94801,94802,94803)")
		f.exec("DELETE FROM company_user_state WHERE user_name='CI_OWNER' AND company_name IN ('CI Firecrawl Due','CI Firecrawl Unsupported')")
		f.exec("DELETE FROM company_identity WHERE id IN (94801,94802,94803)")
	})
	f.exec(`INSERT INTO company_user_state(company_name,user_name,favorite) VALUES
		('CI Firecrawl Due','CI_OWNER',1),('CI Firecrawl Unsupported','CI_OWNER',1)`)
	f.exec(`INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled,auto_enabled,config_json) VALUES
		(94801,'companySite','https://careers.company.com/jobs',1,1,'{}'),
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
}
