//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"itmap/internal/parser"
)

// Только одноразовая CI MariaDB: сценарий 61 событий не использует рабочую БД.
func TestMariaDBSchedulerFunnelNov7Disposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	const (
		firstID   = int64(901000)
		lastID    = int64(901062)
		companyID = int64(1901)
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range []string{
			"DELETE FROM vacancy_import_operations WHERE id LIKE 'ci-go436-%'",
			"DELETE FROM parser_candidate_intakes WHERE candidate_token LIKE 'ci-go436-%'",
			"DELETE FROM vacancy_candidate_identities WHERE candidate_id BETWEEN 901000 AND 901062",
			"DELETE FROM vacancy_candidates WHERE creation_token LIKE 'ci-go436-%'",
			"DELETE FROM company_vacancies WHERE id IN (1901,1902)",
			"DELETE FROM company_crm WHERE company_name='CI GO436 Employer'",
			"DELETE FROM company_identity WHERE id=1901",
		} {
			if _, err := f.db.ExecContext(ctx, statement); err != nil {
				t.Errorf("GO-436 cleanup failed: %v", err)
			}
		}
	})

	f.exec("INSERT INTO company_identity(id,company_name) VALUES(1901,'CI GO436 Employer')")
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI GO436 Employer')")
	f.exec(`INSERT INTO company_vacancies(id,company_name,url,title,created_at) VALUES
		(1901,'CI GO436 Employer','https://example.invalid/ci-go436/new','CI New 436','2026-11-06T21:00:00Z'),
		(1902,'CI GO436 Employer','https://example.invalid/ci-go436/existing','CI Old 436','2026-10-01T00:00:00Z')`)

	// 61 кандидатов получают первый успешный импорт именно 7 ноября по Москве.
	// Ещё два — по одному в соседних сутках: проверить границу 21:00 UTC.
	for i := 0; i < 63; i++ {
		id := firstID + int64(i)
		token := fmt.Sprintf("ci-go436-candidate-%d", i)
		source, method, received := "hh", "apify", "2026-09-01T10:00:00Z"
		at := "2026-11-07T10:00:00Z"
		switch i {
		case 0:
			received = "2026-11-06T20:00:00Z"
			at = "2026-11-06T21:00:00Z" // В Москве уже 07.11 00:00
		case 1:
			at = "2026-11-07T20:59:59Z" // В Москве ещё 07.11
		case 61:
			at = "2026-11-07T21:00:00Z" // В Москве уже 08.11
			method = "firecrawl"
		case 62:
			at = "2026-11-06T10:00:00Z" // 06.11
			source, method = "companySite", "http"
		}
		mainID := int64(1902)
		action := "unchanged"
		if i == 0 {
			mainID = 1901
			action = "create"
		} else if i <= 20 {
			action = "update"
		}
		f.exec(`INSERT INTO vacancy_candidates
			(id,creation_token,company_id,title_raw,title_hash,acquisition_method,
			 first_seen_at,last_seen_at,created_at,updated_at,source_id,main_vacancy_id)
			VALUES(?,?,1901,?,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			 'site',?,?,?,?,?,?)`,
			id, token, fmt.Sprintf("CI GO436 Vacancy %d", i), received,
			received, received, received, source, mainID)
		f.exec(`INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at)
			VALUES(?,?,?,?)`, token, method, source, received)
		f.exec(`INSERT INTO vacancy_import_operations
			(id,candidate_id,vacancy_id,action,status,processed_at,changes_json)
			VALUES(?,?,?,?, 'success',?,'{}')`,
			fmt.Sprintf("ci-go436-import-%d", i), id, mainID, action, at)
	}

	// Два повторных подтверждения и одна ошибка не меняют число первых успехов.
	f.exec(`INSERT INTO vacancy_import_operations
		(id,candidate_id,vacancy_id,action,status,processed_at,changes_json) VALUES
		('ci-go436-retry-0',901000,1901,'unchanged','success','2026-11-07T11:00:00Z','{}'),
		('ci-go436-retry-1',901001,1902,'update','success','2026-11-08T11:00:00Z','{}'),
		('ci-go436-failed-2',901002,1902,'update','failed','2026-11-07T09:00:00Z','{}')`)

	reader := parser.FunnelRepository{
		DB: f.reader,
		Now: func() time.Time { return time.Date(2026, 11, 8, 18, 0, 0, 0, time.UTC) },
	}
	report := func(source, method string) map[string]any {
		t.Helper()
		body, err := reader.ReadFunnel(f.ctx, source, method)
		if err != nil {
			t.Fatalf("read-only funnel query failed: %v", err)
		}
		var value map[string]any
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatalf("invalid funnel JSON: %v", err)
		}
		return value
	}
	findDay := func(value map[string]any, day string) map[string]any {
		t.Helper()
		for _, item := range value["daily"].([]any) {
			if point := item.(map[string]any); point["day"] == day {
				return point
			}
		}
		t.Fatalf("missing day %s", day)
		return nil
	}

	before := dashboardFingerprint(f)
	all := report("all", "all")
	if all["timezone"] != "Europe/Moscow" || len(all["daily"].([]any)) != 30 {
		t.Fatal("funnel calendar or timezone changed")
	}
	if got := findDay(all, "2026-11-07")["published"]; got != float64(61) {
		t.Fatalf("61 successful first imports are not 61 daily events: %v", got)
	}
	if got := findDay(all, "2026-11-08")["published"]; got != float64(1) {
		t.Fatalf("Moscow midnight failed (08.11): %v", got)
	}
	if got := findDay(all, "2026-11-06")["published"]; got != float64(1) {
		t.Fatalf("previous day disappeared: %v", got)
	}
	if got := findDay(report("hh", "apify"), "2026-11-07")["published"]; got != float64(61) {
		t.Fatalf("HH/Apify filter changed 61: %v", got)
	}
	if got := findDay(report("hh", "firecrawl"), "2026-11-07")["published"]; got != float64(0) {
		t.Fatalf("source/parser filtering mixed events: %v", got)
	}
	cohort := all["cohort"].(map[string]any)
	if cohort["received"] != float64(1) || cohort["created"] != float64(1) || cohort["linked"] != float64(0) {
		t.Fatalf("received-date cohort must differ from event-date 61: %+v", cohort)
	}

	// SELECT-only сверка первых успехов. Не считать повтор одной операции новой
	// публикацией и не смешивать число кандидатов с числом основных карточек.
	const sql = `WITH ranked AS (
	  SELECT candidate_id, vacancy_id, action, processed_at,
	    ROW_NUMBER() OVER (PARTITION BY candidate_id ORDER BY processed_at,BINARY id) AS rn
	  FROM vacancy_import_operations
	  WHERE status='success' AND action IN ('create','update','unchanged')
	)
	SELECT COUNT(*),COUNT(DISTINCT r.candidate_id),COUNT(DISTINCT r.vacancy_id),
	  COUNT(DISTINCT c.main_vacancy_id),
	  SUM(r.action='create'),SUM(r.action='update'),SUM(r.action='unchanged')
	FROM ranked r
	JOIN vacancy_candidates c ON c.id=r.candidate_id
	JOIN parser_candidate_intakes i ON i.candidate_token=c.creation_token
	WHERE r.rn=1 AND r.candidate_id BETWEEN ? AND ?
	  AND i.source_id='hh' AND i.parser='apify'
	  AND r.processed_at >= ? AND r.processed_at < ?`
	var events, candidates, operationMain, linkedMain, created, updated, unchanged int64
	err := f.reader.QueryRowContext(f.ctx, sql, firstID, lastID,
		"2026-11-06T21:00:00Z", "2026-11-07T21:00:00Z").
		Scan(&events, &candidates, &operationMain, &linkedMain, &created, &updated, &unchanged)
	if err != nil {
		t.Fatalf("reconciliation SELECT failed: %v", err)
	}
	if events != 61 || candidates != 61 || operationMain != 2 || linkedMain != 2 ||
		created != 1 || updated != 20 || unchanged != 40 {
		t.Fatalf("GO-436 mismatch: events=%d candidates=%d mainOp=%d mainLinked=%d create=%d update=%d unchanged=%d",
			events, candidates, operationMain, linkedMain, created, updated, unchanged)
	}
	if before != dashboardFingerprint(f) {
		t.Fatal("read-only diagnostics changed MariaDB")
	}
	t.Log("PASS: synthetic 61 published import events = 61 candidates = 2 main vacancies = 1 create + 20 updates + 40 unchanged, with retries, failed receipt, 30-day cohort and MSK boundaries; NO production evidence")
}
