package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"itmap/internal/apifysettings"
	"itmap/internal/profiles"
)

// relevanceSnapshot содержит личные данные только в памяти защищённой стадии.
type relevanceSnapshot struct {
	CandidateID, CompanyID, VacancyID              int64
	Owner, SourceURL, CreatedAt, Title, Markdown   string
	VacancyURL, Level, RoleText                    string
	PublishedAt, LoadedAt                          sql.NullString
	SalaryFrom, SalaryTo                           *int64
	Resume, Prompt                                 string
	ResumeRevision, PromptRevision, RouterRevision sql.NullInt64
	Config                                         profiles.ModelInput
	Score                                          sql.NullInt64
	Roles                                          []int64
	RoleDetails                                    []relevanceRole
	Key, InputHash, BaseKey, BaseHash, RolesHash   string
	Eligible                                       bool
}

type relevanceRole struct {
	ID                    int64
	Name, StableKey, Path string
}

func sqlNullableString(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}
func sqlNullableInt(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}

// readRelevanceSnapshot повторно определяет scope из БД, не из payload задания.
func readRelevanceSnapshot(ctx context.Context, tx *sql.Tx, job Job, owner string) (relevanceSnapshot, error) {
	var x relevanceSnapshot
	if tx == nil || owner == "" || job.CandidateID == nil || job.CompanyID == nil || job.AgencyID != nil || job.SourceID == nil || *job.SourceID != "hh" {
		return x, ErrJobInput
	}
	x.Owner = owner
	x.CandidateID = *job.CandidateID
	x.CompanyID = *job.CompanyID
	x.Roles = []int64{}
	x.RoleDetails = []relevanceRole{}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return x, ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return x, ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != owner {
		return x, ErrCandidateChanged
	}
	selected := false
	for _, source := range settings.SourceIDs {
		selected = selected || source == "hh"
	}
	if !selected {
		return x, ErrCandidateChanged
	}
	source, err := (apifysettings.Repository{}).ReadHHSourceTx(ctx, tx, x.CompanyID)
	if err != nil {
		return x, err
	}
	if !source.Enabled || !source.Supported {
		return x, apifysettings.ErrDisabled
	}
	var candidateCompany sql.NullInt64
	var candidateAgency sql.NullInt64
	var candidateSource, candidateSourceURL sql.NullString
	var vacancy sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT company_id,agency_id,source_id,source_url,main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE", x.CandidateID).Scan(&candidateCompany, &candidateAgency, &candidateSource, &candidateSourceURL, &vacancy)
	if err != nil || !candidateCompany.Valid || candidateCompany.Int64 != x.CompanyID || candidateAgency.Valid || !candidateSource.Valid || candidateSource.String != "hh" || !candidateSourceURL.Valid || candidateSourceURL.String != source.URL || !vacancy.Valid || vacancy.Int64 <= 0 {
		return x, ErrCandidateChanged
	}
	x.VacancyID = vacancy.Int64
	x.SourceURL = source.URL
	var company string
	if tx.QueryRowContext(ctx, "SELECT company_name FROM company_identity WHERE id=? FOR UPDATE", x.CompanyID).Scan(&company) != nil || company == "" {
		return x, ErrCandidateChanged
	}
	if row.favorite != 0 {
		var favorite int
		err = tx.QueryRowContext(ctx, "SELECT favorite FROM company_user_state WHERE company_name=? AND user_name=? FOR UPDATE", company, owner).Scan(&favorite)
		if errors.Is(err, sql.ErrNoRows) || err == nil && favorite != 1 {
			return x, ErrCandidateChanged
		}
		if err != nil {
			return x, ErrSettingsStorage
		}
	}
	var name, entity string
	var archived int64
	var from, to sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT company_name,entity_type,created_at,title,description_md,salary_from,salary_to,archived,url,level,role,published_at,loaded_at FROM company_vacancies WHERE id=? FOR UPDATE", x.VacancyID).Scan(&name, &entity, &x.CreatedAt, &x.Title, &x.Markdown, &from, &to, &archived, &x.VacancyURL, &x.Level, &x.RoleText, &x.PublishedAt, &x.LoadedAt)
	if err != nil || name != company || entity != "company" || archived != 0 {
		return x, ErrCandidateChanged
	}
	if from.Valid {
		v := from.Int64
		x.SalaryFrom = &v
	}
	if to.Valid {
		v := to.Int64
		x.SalaryTo = &v
	}
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=? ORDER BY role_id FOR UPDATE", x.VacancyID)
	if err != nil {
		return x, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		x.Roles = append(x.Roles, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return x, ErrSettingsStorage
	}
	if len(x.Roles) > 0 {
		catalog, err := leafRoleCatalog(ctx, tx)
		if err != nil {
			return x, err
		}
		leaves := map[int64]relevanceRole{}
		for _, role := range catalog.Leaves {
			leaves[role.ID] = relevanceRole{role.ID, role.Name, role.StableKey, role.Path}
		}
		for _, id := range x.Roles {
			role, ok := leaves[id]
			if !ok {
				return x, ErrRelevanceChanged
			}
			x.RoleDetails = append(x.RoleDetails, role)
		}
	}
	var resume, prompt, provider, openai, openrouter, reasoning sql.NullString
	var modelRev sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT resume_md,revision FROM user_profiles WHERE user_name=? FOR UPDATE", owner).Scan(&resume, &x.ResumeRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	err = tx.QueryRowContext(ctx, "SELECT markdown,revision FROM system_prompts WHERE kind='relevance' FOR UPDATE").Scan(&prompt, &x.PromptRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	err = tx.QueryRowContext(ctx, "SELECT provider,openai_model,openrouter_model,reasoning_effort,revision FROM prompt_model_settings WHERE kind='relevance' FOR UPDATE").Scan(&provider, &openai, &openrouter, &reasoning, &modelRev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	err = tx.QueryRowContext(ctx, "SELECT revision FROM openrouter_settings WHERE id=1 FOR UPDATE").Scan(&x.RouterRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	err = tx.QueryRowContext(ctx, "SELECT relevance FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=? FOR UPDATE", owner, x.VacancyID).Scan(&x.Score)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	x.Resume = resume.String
	x.Prompt = prompt.String
	x.Config = profiles.ModelInput{Provider: provider.String, OpenAIModel: openai.String, OpenRouterModel: openrouter.String, ReasoningEffort: reasoning.String, Revision: modelRev.Int64}
	snapshot := []any{job.Generation, x.VacancyID, x.CreatedAt, x.Title, x.Markdown, sqlNullableInt(from), sqlNullableInt(to), sqlNullableString(resume), sqlNullableInt(x.ResumeRevision), sqlNullableString(prompt), sqlNullableInt(x.PromptRevision), sqlNullableString(provider), sqlNullableString(openai), sqlNullableString(openrouter), sqlNullableString(reasoning), sqlNullableInt(modelRev), sqlNullableInt(x.RouterRevision), sqlNullableInt(x.Score)}
	x.InputHash, err = canonicalHash(snapshot)
	if err != nil {
		return x, err
	}
	x.Key = fmt.Sprintf("relevance:%s:%d:%s", owner, x.VacancyID, x.InputHash)
	snapshot[len(snapshot)-1] = nil
	x.BaseHash, err = canonicalHash(snapshot)
	if err != nil {
		return x, err
	}
	x.BaseKey = fmt.Sprintf("relevance:%s:%d:%s", owner, x.VacancyID, x.BaseHash)
	x.RolesHash, err = canonicalHash(x.RoleDetails)
	if err != nil {
		return x, err
	}
	x.Eligible = !x.Score.Valid && len(x.Roles) > 0 && jsStageTrim(x.Resume) != "" && jsStageTrim(x.Prompt) != "" && jsStageTrim(x.Title) != "" && jsStageTrim(x.Markdown) != "" && x.Config.Provider == "openrouter" && x.Config.OpenRouterModel == "jev" && profiles.ValidateModel("relevance", x.Config) == nil
	return x, nil
}
