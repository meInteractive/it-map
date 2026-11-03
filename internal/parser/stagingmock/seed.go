package stagingmock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/aihttp"
)

// Seed наполняет только пустую явно указанную БД синтетическими данными.
// После частичного отказа она требует ручной проверки, не стирая данные.
func Seed(ctx context.Context, db *sql.DB, secret, expectedDatabase string) error {
	if db == nil || secret == "" || expectedDatabase == "" {
		return errors.New("staging mock: отсутствуют параметры подготовки")
	}
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&actual); err != nil || actual != expectedDatabase {
		return errors.New("staging mock: неверная база")
	}
	var generation string
	if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil {
		return errors.New("staging mock: отсутствует поколение базы")
	}
	tables := []string{"company_identity", "app_catalog", "apify_settings", "vacancy_source_parsing", "firecrawl_settings", "vacancy_roles", "vacancy_role_classification", "vacancy_detail_settings", "vacancy_role_settings", "user_profiles", "system_prompts", "prompt_model_settings", "openrouter_settings", "openrouter_ai_logs", "scheduler_runs", "scheduler_jobs", "scheduler_attempts", "company_vacancies", "vacancy_candidates"}
	for _, table := range tables {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&n); err != nil || n != 0 {
			return fmt.Errorf("staging mock: таблица %s не пуста или недоступна", table)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("staging mock: транзакция недоступна")
	}
	defer tx.Rollback()
	statements := []string{
		`INSERT INTO company_identity(id,company_name) VALUES(991302,'CI Apify Runtime')`,
		`INSERT INTO app_catalog(id,data_json) VALUES(1,'{"companies":[{"name":"CI Apify Runtime","vacancySources":{"hh":"https://hh.ru/employer/456"}}],"agencies":[]}')`,
		`INSERT INTO firecrawl_settings(id,favorites_only,scheduler_json) VALUES(1,0,'{"enabled":true,"sourceIds":["hh"],"ownerName":"CI_OWNER","dayBudgetUsd":5,"monthBudgetUsd":5}')`,
		`INSERT INTO vacancy_roles(id,name,stable_key,sort_order) VALUES(991303,'CI HH Root','root-it-manager',1)`,
		`INSERT INTO vacancy_roles(id,name,stable_key,parent_id,sort_order) VALUES(991304,'CI HH leaf','ci-hh-leaf',991303,1)`,
		`INSERT INTO vacancy_role_classification(role_id,markdown,parsing_enabled) VALUES(991303,'CI HH classification',1)`,
		`INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision) VALUES(1,'STAGING extraction prompt',1,'openai','gpt-6-luna','medium',1)`,
		`INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision) VALUES(1,'STAGING roles prompt',1,'openai','gpt-6-luna','best_free','medium',1)`,
		`INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('CI_OWNER','CI synthetic resume',1)`,
		`INSERT INTO system_prompts(kind,markdown,revision) VALUES('relevance','CI relevance prompt',1)`,
		`INSERT INTO prompt_model_settings(kind,provider,openai_model,openrouter_model,reasoning_effort,revision) VALUES('relevance','openrouter','','jev','medium',1)`,
		`INSERT INTO openrouter_settings(id,revision) VALUES(1,1)`,
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return errors.New("staging mock: не удалось сохранить синтетические настройки")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("staging mock: результат сохранения неизвестен")
	}
	settings := apifysettings.NewRepository(db, secret)
	if _, err := settings.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "free", APIKey: "apify_api_" + strings.Repeat("s", 16), Revision: 0}); err != nil {
		return errors.New("staging mock: подготовка синтетического Apify прервана; нужна проверка базы")
	}
	if _, err := settings.SaveSettings(ctx, generation, apifysettings.SettingsInput{Enabled: true, Revision: 1}); err != nil {
		return errors.New("staging mock: настройка синтетического Apify прервана; нужна проверка базы")
	}
	if _, err := settings.SaveHHSource(ctx, generation, 991302, apifysettings.HHSourceInput{SourceURL: SourceURL, Enabled: true, Revision: 0}); err != nil {
		return errors.New("staging mock: подготовка источника прервана; нужна проверка базы")
	}
	keys := aiproviders.NewRepository(db, secret, "sk-"+strings.Repeat("s", 16))
	if err := keys.SaveKey(ctx, aihttp.OpenRouter, generation, "sk-or-"+strings.Repeat("r", 16)); err != nil {
		return errors.New("staging mock: подготовка синтетического AI прервана; нужна проверка базы")
	}
	return nil
}
