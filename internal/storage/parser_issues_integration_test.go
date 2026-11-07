//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"itmap/internal/parser"
)

// Тест выполняется исключительно на одноразовой itmap_ci с проверенным nonce.
func TestMariaDBSchedulerErrorHistoryIssuesDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	for _, table := range []string{"parser_error_daily", "vacancy_role_classification", "company_vacancies"} {
		var count int
		if err := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("issues fixture requires an empty private namespace")
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range []string{"parser_error_daily", "company_vacancies"} {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("issues cleanup failed: ", table)
			}
		}
		if _, err := f.db.ExecContext(ctx, "DELETE FROM company_crm WHERE company_name IN ('СБЕР','sber')"); err != nil {
			t.Error("issues cleanup failed: company_crm")
		}
	})
	const settings = `{"enabled":true,"ownerName":"Eugene","sourceIds":["hh"],"favoritesOnly":false,"agenciesEnabled":false,"intervalDays":7,"vacancyAgeDays":3,"dayBudgetUsd":5,"monthBudgetUsd":10,"revision":1}`
	f.exec("INSERT INTO firecrawl_settings(id,scheduler_json,favorites_only,revision) VALUES(1,?,0,1)", settings)
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(1,'CI Issues Employer')")
	// company_vacancies ссылается на company_crm: тестовые компании нужны до вакансий.
	f.exec("INSERT INTO company_crm(company_name) VALUES('СБЕР'),('sber')")
	f.exec("INSERT INTO company_vacancies(company_name,title,url,loaded_at) VALUES('СБЕР','CI previous role','https://example.invalid/ci-sber-legacy','2026-10-20')")
	f.exec("INSERT INTO company_vacancies(company_name,title,url,loaded_at) VALUES('sber','CI previous role 2','https://example.invalid/ci-sber-legacy-2','2026-10-20T22:00:00Z')")
	f.exec("INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled) VALUES(1,'hh','https://example.invalid/ci',1)")
	base := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	now := base.UnixMilli()
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('ci_old_signal',?,'automatic',?,'completed')", f.generation, now-2*3600000)
	add := func(id, kind, status, reason string, candidate any, updated int64) {
		f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,status,reason_code,priority,run_after,created_at,updated_at)
			VALUES(?,?,?,?,1,?,'hh',?,?,0,0,?,?)`, id, f.generation, id, kind, candidate, status, reason, updated, updated)
	}
	add("ci_old_scan_failed", "scan_source", "failed", "extraction", nil, now-10000)
	add("ci_recovered_scan", "scan_source", "completed", "", nil, now-9000)
	add("ci_active_scan_failed", "scan_source", "failed", "extraction", nil, now-8000)
	add("ci_expected_wait", "fetch_full_text", "pending", "apify_running", 56, now-7000)
	add("ci_pending_blocked", "classify_title", "pending", "stage_failed", 55, now-6000)
	add("ci_old_extract_failed", "extract_vacancy", "failed", "storage", 57, now-5000)
	add("ci_completed_extract", "extract_vacancy", "completed", "", 57, now-4000)

	repo := parser.IssuesRepository{DB: f.db, Now: func() time.Time { return base }}
	read := func() map[string]any {
		t.Helper()
		raw, err := repo.ReadIssues(f.ctx)
		if err != nil {
			t.Fatal("CI issues read", err)
		}
		var report map[string]any
		if json.Unmarshal(raw, &report) != nil {
			t.Fatal("invalid issues JSON")
		}
		return report
	}
	check := func(got map[string]any, p1, p2 float64, days float64) {
		t.Helper()
		counts := got["activeCounts"].(map[string]any)
		if counts["p1"] != p1 || counts["p2"] != p2 {
			t.Fatalf("active current P1/P2 mismatch: %+v, expected %.0f/%.0f", counts, p1, p2)
		}
		history := got["history"].(map[string]any)
		if history["observedDays"] != days {
			t.Fatalf("missing days became zero: %+v", history["observedDays"])
		}
	}
	// READ не записывает снимки, без открытой страницы архив остаётся без подставных нулей.
	first := read()
	check(first, 2, 2, 0)
	byID := map[string]map[string]any{}
	for _, raw := range first["issues"].([]any) {
		item := raw.(map[string]any)
		byID[item["id"].(string)] = item
	}
	if byID["sber-levels"]["status"] != "verification" || byID["sber-levels"]["affected"] != float64(2) ||
		byID["yoomoney-cycle"]["status"] != "verification" {
		t.Fatal("Go issues report lost historical JS verification backlog")
	}
	if byID["sber-levels"]["priority"] != float64(2) || byID["yoomoney-cycle"]["priority"] != float64(2) {
		t.Fatal("verification entries must remain separate from active P1/P2 counts")
	}
	if err := repo.Sample(f.ctx); err != nil {
		t.Fatal("automated snapshot failed", err)
	}
	next := read()
	check(next, 2, 2, 1)
	if next["historicalFailed"] != float64(3) {
		t.Fatal("historical failure count omitted recovered jobs")
	}
	history := next["history"].(map[string]any)
	if history["average"].(map[string]any)["p2"] != float64(2) {
		t.Fatal("sampled daily mean differs")
	}
	// Новый успешный обход подавляет прежний scan, но не скрывает независимое ожидание.
	f.exec("UPDATE vacancy_source_parsing SET last_success_at=? WHERE company_id=1", base.Add(time.Minute).UTC().Format(time.RFC3339Nano))
	f.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.enabled',false) WHERE id=1")
	next = read()
	check(next, 0, 1, 1)
	if err := repo.Sample(f.ctx); err != nil {
		t.Fatal("subsequent sample failed", err)
	}
	next = read()
	check(next, 0, 1, 1)
	if next["history"].(map[string]any)["average"].(map[string]any)["p2"] != float64(2) {
		t.Fatal("daily maximum was replaced with most recent count")
	}
	// Граница Москвы: 7 ноября 23:59 МСК -> 8 ноября 00:01 МСК.
	repo.Now = func() time.Time { return time.Date(2026, 11, 7, 21, 1, 0, 0, time.UTC) }
	if err := repo.Sample(f.ctx); err != nil {
		t.Fatal("new Moscow day sample failed", err)
	}
	next = read()
	check(next, 0, 1, 2)
	history = next["history"].(map[string]any)
	if history["average"].(map[string]any)["p2"] != float64(1.5) {
		t.Fatal("average must be based on observed days only")
	}
	// Список источников и избранное ограничивают область, а не подменяют историю.
	f.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
	next = read()
	check(next, 0, 0, 2)
	f.exec("UPDATE firecrawl_settings SET favorites_only=0, scheduler_json=JSON_SET(scheduler_json,'$.sourceIds',JSON_ARRAY('companySite')) WHERE id=1")
	next = read()
	check(next, 0, 0, 2)
	// SELECT-only reader не может записать историю, ошибка не возвращается как ноль.
	readonly := parser.IssuesRepository{DB: f.reader, Now: repo.Now}
	if _, err := readonly.ReadIssues(f.ctx); err != nil {
		t.Fatal("issues report must be readable with SELECT-only rights", err)
	}
	if err := readonly.Sample(f.ctx); err == nil {
		t.Fatal("SELECT-only sampler unexpectedly changed daily history")
	}
	t.Log("PASS: active signal scope, overdue P1, suppressed retries, missing days, two Moscow dates, maxima, read-only failure")
}
