package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const issuesJobLimit = 120

// IssuesRepository читает только локальную MariaDB; ни один метод не вызывает провайдеров.
type IssuesRepository struct {
	DB  *sql.DB
	Now func() time.Time
}

type parserIssueJob struct {
	ID          string `json:"id"`
	CompanyName string `json:"companyName"`
	AgencyID    *int64 `json:"agencyId"`
	CandidateID *int64 `json:"candidateId"`
	Status      string `json:"status"`
	RunAfter    int64  `json:"runAfter"`
	Message     string `json:"message"`
}
type parserIssue struct {
	ID          string           `json:"id"`
	Priority    int              `json:"priority"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	Description string           `json:"description"`
	NextAction  string           `json:"nextAction"`
	Target      string           `json:"target"`
	ObservedAt  *int64           `json:"observedAt,omitempty"`
	ReviewedAt  string           `json:"reviewedAt,omitempty"`
	SourceID    string           `json:"sourceId,omitempty"`
	Stage       string           `json:"stage,omitempty"`
	ReasonCode  string           `json:"reasonCode,omitempty"`
	Jobs        []parserIssueJob `json:"jobs,omitempty"`
	Affected    int64            `json:"affected,omitempty"`
}
type parserIssuesReport struct {
	CheckedAt        int64              `json:"checkedAt"`
	Issues           []parserIssue      `json:"issues"`
	HistoricalFailed int64              `json:"historicalFailed"`
	SampleLimited    bool               `json:"sampleLimited"`
	Limit            int                `json:"limit"`
	ActiveCounts     ErrorHistoryCounts `json:"activeCounts"`
	History          json.RawMessage    `json:"history,omitempty"`
}

var expectedIssueWait = map[string]bool{
	"": true, "not_due": true, "source_scope": true, "scope": true,
	"agencies_disabled": true, "disabled": true, "apify_disabled": true,
	"apify_running": true, "apify_busy": true, "apify_budget": true,
	"apify_cycle_limit": true, "time_slice": true, "slice": true,
	"scheduler_budget": true, "rate_limit": true, "provider_busy": true,
	"concurrency": true, "budget": true, "budget_daily": true,
	"budget_monthly": true, "lease_busy": true,
}
var issueActions = map[string]string{
	"storage": "Проверьте сохранённый прогресс и ошибку в очереди перед повтором.",
	"extraction": "Проверьте правило извлечения вакансий и сохранённую порцию.",
	"roles_pending": "Проверьте справочник дочерних ролей и результат назначения.",
	"roles_unresolved": "Проверьте результат назначения ролей перед импортом.",
	"budget": "Проверьте остаток и резервы в разделе лимитов.",
	"hh_roles_missing": "Выберите профессиональные роли HH и проверьте область сканирования.",
}

func issueAction(code string) string {
	if action, ok := issueActions[code]; ok {
		return action
	}
	return "Откройте очередь и проверьте последнюю попытку. Не повторяйте платную операцию без проверки её состояния."
}

func issueReason(code string) string {
	if code == "" {
		return "не указана"
	}
	for _, r := range code {
		if r > 127 || r < 'a' && (r < '0' || r > '9') || r > 'z' && r != '_' && r != '-' {
			return "unknown"
		}
	}
	if len(code) > 64 {
		return "unknown"
	}
	return code
}

// Исторический успешный scan отменяет прежний сигнал об ошибке только при
// достоверной дате ISO; повреждённая дата не превращается в фиктивное исправление.
func issueRecovered(kind string, updatedAt int64, lastSuccess sql.NullString) bool {
	if kind != "scan_source" || !lastSuccess.Valid {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, lastSuccess.String)
	return err == nil && at.UnixMilli() >= updatedAt
}

func (r IssuesRepository) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// collect возвращает точные активные сигналы, не суммируя retries и завершённые
// задания. Транзакция только для чтения, без сетевых и платных вызовов.
func (r IssuesRepository) collect(ctx context.Context, now time.Time) (report parserIssuesReport, owner string, err error) {
	report = parserIssuesReport{CheckedAt: now.UnixMilli(), Issues: []parserIssue{}, Limit: issuesJobLimit}
	if r.DB == nil {
		return report, "", ErrSettingsStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return report, "", settingsFailure(ctx, err)
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return report, "", ErrSettingsStorage
	}
	row, err := readSettingsRow(ctx, tx, false)
	if err != nil {
		return report, "", settingsFailure(ctx, err)
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return report, "", settingsFailure(ctx, err)
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || settings.OwnerName == "" {
		return report, "", ErrSettingsStorage
	}
	owner = settings.OwnerName
	add := func(id string, priority int, title, status, description, action, target string) {
		report.Issues = append(report.Issues, parserIssue{
			ID: id, Priority: priority, Title: title, Status: status,
			Description: description, NextAction: action, Target: target,
		})
	}
	var signalAt sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT MAX(started_at) FROM scheduler_runs WHERE generation=? AND `trigger`='automatic'", generation).Scan(&signalAt); err != nil {
		return report, "", settingsFailure(ctx, err)
	}
	if settings.Enabled && signalAt.Valid && now.UnixMilli()-signalAt.Int64 >= 75*60*1000 {
		add("cloud-overdue", 1, "Не поступает автоматический сигнал", "observed",
			"Последний автоматический запуск старше 75 минут.", "Проверьте штатный сигнализатор и очередь, не создавая повторный платный запуск.", "connection")
		report.Issues[len(report.Issues)-1].ObservedAt = &signalAt.Int64
	}
	if settings.Enabled && len(settings.SourceIDs) > 0 {
		var active int64
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vacancy_role_classification c JOIN vacancy_roles v ON v.id=c.role_id WHERE v.parent_id IS NULL AND c.parsing_enabled=1 AND CHAR_LENGTH(TRIM(c.markdown))>0").Scan(&active)
		if err != nil {
			return report, "", settingsFailure(ctx, err)
		}
		if active == 0 {
			add("no-active-roles", 1, "Нет активных направлений с промптом", "observed",
				"Отбор новых вакансий не сможет продолжиться.", "Включите корневую роль и сохраните промпт.", "scope")
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND status='failed'", generation).Scan(&report.HistoricalFailed); err != nil {
		return report, "", settingsFailure(ctx, err)
	}

	if len(settings.SourceIDs) > 0 {
		placeholders := make([]string, len(settings.SourceIDs))
		args := []any{generation}
		for i, source := range settings.SourceIDs {
			placeholders[i] = "?"
			args = append(args, source)
		}
		agencies := 0
		if settings.AgenciesEnabled {
			agencies = 1
		}
		args = append(args, agencies, row.favorite, owner)
		// В отличие от SQLite IS, сравнение <=> сохраняет отдельные namespace
		// для company/agency даже при одинаковых числовых идентификаторах.
		query := `SELECT j.id,j.type,j.source_id,j.company_id,j.agency_id,j.candidate_id,
			j.status,j.reason_code,j.updated_at,j.run_after,
			CASE WHEN j.agency_id IS NOT NULL THEN a.agency_name ELSE c.company_name END AS company_name,
			(SELECT MAX(s.last_success_at) FROM vacancy_source_parsing s WHERE s.enabled=1
			 AND BINARY s.source_id=BINARY j.source_id
			 AND s.company_id<=>j.company_id AND s.agency_id<=>j.agency_id) AS last_success_at
			FROM scheduler_jobs j
			LEFT JOIN company_identity c ON c.id=j.company_id
			LEFT JOIN agency_identity a ON a.id=j.agency_id
			WHERE j.generation=? AND j.status IN ('failed','pending')
			AND j.source_id IN (` + strings.Join(placeholders, ",") + `)
			AND (j.company_id IS NOT NULL OR (?=1 AND j.agency_id IS NOT NULL))
			AND (CASE WHEN j.agency_id IS NOT NULL THEN a.agency_name ELSE c.company_name END) IS NOT NULL
			AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u
			 WHERE u.company_name=CASE WHEN j.agency_id IS NOT NULL THEN a.agency_name ELSE c.company_name END
			 AND u.user_name=? AND u.favorite=1))
			AND EXISTS(SELECT 1 FROM vacancy_source_parsing s WHERE s.enabled=1
			 AND BINARY s.source_id=BINARY j.source_id
			 AND s.company_id<=>j.company_id AND s.agency_id<=>j.agency_id)
			AND NOT EXISTS(SELECT 1 FROM scheduler_jobs newer
			 WHERE newer.generation=j.generation AND newer.type=j.type
			 AND newer.company_id<=>j.company_id AND newer.agency_id<=>j.agency_id
			 AND newer.source_id<=>j.source_id AND newer.candidate_id<=>j.candidate_id
			 AND newer.status IN ('completed','running','pending')
			 AND (newer.updated_at>j.updated_at OR (newer.updated_at=j.updated_at AND BINARY newer.id>BINARY j.id)))
			ORDER BY j.updated_at DESC,BINARY j.id DESC`
		rows, queryErr := tx.QueryContext(ctx, query, args...)
		if queryErr != nil {
			return report, "", settingsFailure(ctx, queryErr)
		}
		groups := map[string]int{}
		sourceNames := map[string]string{}
		for _, option := range SourceOptions() {
			sourceNames[option.ID] = option.Name
		}
		for rows.Next() {
			var id, kind, source, status, rawReason string
			var company, agency, candidate sql.NullInt64
			var name, lastSuccess sql.NullString
			var updatedAt, runAfter int64
			if err = rows.Scan(&id, &kind, &source, &company, &agency, &candidate, &status, &rawReason, &updatedAt, &runAfter, &name, &lastSuccess); err != nil {
				break
			}
			if status == "pending" && expectedIssueWait[rawReason] || issueRecovered(kind, updatedAt, lastSuccess) {
				continue
			}
			report.ActiveCounts.P2++
			if report.ActiveCounts.P2 > issuesJobLimit {
				report.SampleLimited = true
				continue
			}
			reason := issueReason(rawReason)
			key := kind + ":" + source + ":" + reason
			index, ok := groups[key]
			if !ok {
				label := sourceNames[source]
				if label == "" {
					label = source
				}
				add("queue:"+key, 2, label+" · Требует проверки", "observed",
					"Последнее доступное состояние содержит ошибку или проблемное ожидание. Это не обязательно отказ всего источника.",
					issueAction(reason), "queue")
				index = len(report.Issues)-1
				report.Issues[index].SourceID = source
				report.Issues[index].Stage = kind
				report.Issues[index].ReasonCode = reason
				report.Issues[index].Jobs = []parserIssueJob{}
				groups[key] = index
			}
			issue := &report.Issues[index]
			if issue.ObservedAt == nil || updatedAt > *issue.ObservedAt {
				at := updatedAt
				issue.ObservedAt = &at
			}
			if len(issue.Jobs) < 8 {
				job := parserIssueJob{ID: id, Status: status, RunAfter: runAfter, CompanyName: name.String, Message: issueAction(reason)}
				if agency.Valid {
					v := agency.Int64
					job.AgencyID = &v
				}
				if candidate.Valid {
					v := candidate.Int64
					job.CandidateID = &v
				}
				issue.Jobs = append(issue.Jobs, job)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return report, "", settingsFailure(ctx, err)
		}
		for _, index := range groups {
			n := len(report.Issues[index].Jobs)
			report.Issues[index].Description = fmt.Sprintf("Зафиксировано до %d последних заданий этой группы; полный список доступен в очереди.", n)
		}
	}
	// Поддерживаемый перечень проверок отделён от фактических P1/P2.
	add("hh-completeness", 2, "HH: проверить частичные результаты Apify", "verification",
		"Незавершённые Actor могли вернуть частичный Dataset; требуется отдельная сверка.", "Сверьте Actor и сохранённые кандидаты, не запуская новый Actor.", "source")
	add("cost-attribution", 3, "Сверить списания ИИ и неопределённые резервы", "planned",
		"Оценка бюджета может включать незавершённые резервы.", "Сопоставьте attempt receipts и подтверждения провайдера без новых платных вызовов.", "services")
	add("history-reconciliation", 3, "Сверить историю ошибок и исправления", "planned",
		"Исторические failed остаются в журнале после последующих успешных заданий.", "Сравните актуальную диагностику и архив очереди.", "queue")
	sort.SliceStable(report.Issues, func(i, j int) bool { return report.Issues[i].Priority < report.Issues[j].Priority })
	report.ActiveCounts.P1 = 0
	for _, issue := range report.Issues {
		if issue.Priority == 1 && issue.Status == "observed" {
			report.ActiveCounts.P1++
		}
	}
	if err = tx.Commit(); err != nil {
		return report, "", settingsFailure(ctx, err)
	}
	return report, owner, nil
}

// ReadIssues не пишет дневную историю: чтение страницы не создаёт наблюдения.
func (r IssuesRepository) ReadIssues(ctx context.Context) (json.RawMessage, error) {
	now := r.clock()
	report, owner, err := r.collect(ctx, now)
	if err != nil {
		return nil, settingsFailure(ctx, err)
	}
	op, err := (ErrorHistoryRepository{DB: r.DB}).PrepareHistory(ctx, owner)
	if err != nil {
		return nil, settingsFailure(ctx, err)
	}
	report.History, err = op.Read(ctx, now.UnixMilli())
	if err != nil {
		return nil, settingsFailure(ctx, err)
	}
	raw, err := json.Marshal(report)
	return raw, settingsFailure(ctx, err)
}

// Sample вызывается только штатным защищённым тиком планировщика.
func (r IssuesRepository) Sample(ctx context.Context) error {
	now := r.clock()
	report, owner, err := r.collect(ctx, now)
	if err != nil {
		return settingsFailure(ctx, err)
	}
	op, err := (ErrorHistoryRepository{DB: r.DB}).PrepareHistory(ctx, owner)
	if err != nil {
		return settingsFailure(ctx, err)
	}
	return settingsFailure(ctx, op.Record(ctx, report.ActiveCounts, now.UnixMilli()))
}
