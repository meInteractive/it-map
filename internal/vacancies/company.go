package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"itmap/internal/roles"
)

// CompanyVacancy matches the v316 readVacancies shape, including fields that
// are intentionally omitted from the compact /api/vacancies/list endpoint.
type CompanyVacancy struct {
	Vacancy
	URL                string                 `json:"url"`
	DescriptionMD      string                 `json:"descriptionMd"`
	CoverLetterSubject string                 `json:"coverLetterSubject"`
	CoverLetterMD      string                 `json:"coverLetterMd"`
	CoverLetterScope   string                 `json:"coverLetterScope"`
	LastCheckedAt      *string                `json:"lastCheckedAt"`
	UpdatedAt          string                 `json:"updatedAt"`
	TelegramSources    []DetailTelegramSource `json:"telegramSources"`
}

// CompanyVacancies reads at most 100 vacancies linked to a company, preserving
// the full legacy drawer payload. It performs no lazy initialization on GET.
// All vacancy, user-state, letter, role and source rows are read in one
// repeatable-read transaction; a separate immutable role directory supplies
// display paths.
func (r Repository) CompanyVacancies(ctx context.Context, user, company string) ([]CompanyVacancy, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || strings.TrimSpace(company) == "" {
		return nil, errors.New("vacancy DB, user or company unavailable")
	}
	directory, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return nil, err
	}
	knownRoles := make(map[int64]roles.Role, len(directory.Roles))
	for _, role := range directory.Roles {
		knownRoles[role.ID] = role
	}

	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	const q = `SELECT v.id,v.company_name,v.entity_type,COALESCE(v.real_company_name,''),
      COALESCE(v.title,''),v.salary_from,v.salary_to,COALESCE(v.role,''),COALESCE(v.level,''),
      (SELECT s.relevance FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),
      COALESCE((SELECT s.favorite FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),0),
      COALESCE((SELECT s.hidden FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),0),
      v.published_at,v.loaded_at,v.last_checked_at,COALESCE(v.archived,0),COALESCE(v.created_at,''),COALESCE(v.updated_at,''),
      (SELECT a.id FROM company_applications a WHERE a.user_name=? AND a.vacancy_id=v.id ORDER BY a.updated_at DESC,a.id DESC LIMIT 1),
      COALESCE((SELECT a.status FROM company_applications a WHERE a.user_name=? AND a.vacancy_id=v.id ORDER BY a.updated_at DESC,a.id DESC LIMIT 1),''),
      COALESCE(v.url,''),COALESCE(v.description_md,''),COALESCE(v.cover_letter_subject,''),COALESCE(v.cover_letter_md,'')
      FROM company_vacancies v
      WHERE v.company_name=? OR v.real_company_name=?
      ORDER BY v.published_at IS NULL,v.published_at DESC,v.id DESC LIMIT 100`
	rows, err := tx.QueryContext(ctx, q, user, user, user, user, user, company, company)
	if err != nil {
		return nil, err
	}
	result := make([]CompanyVacancy, 0)
	index := make(map[int64]int)
	for rows.Next() {
		var v CompanyVacancy
		var salaryFrom, salaryTo, relevance, appID sql.NullInt64
		var published, loaded, checked sql.NullString
		var favorite, hidden, archived int64
		err = rows.Scan(&v.ID, &v.CompanyName, &v.EntityType, &v.RealCompanyName, &v.Title,
			&salaryFrom, &salaryTo, &v.Role, &v.Level, &relevance, &favorite, &hidden,
			&published, &loaded, &checked, &archived, &v.CreatedAt, &v.UpdatedAt,
			&appID, &v.ApplicationStatus, &v.URL, &v.DescriptionMD, &v.CoverLetterSubject, &v.CoverLetterMD)
		if err != nil {
			break
		}
		v.SalaryFrom = nullableInt(salaryFrom)
		v.SalaryTo = nullableInt(salaryTo)
		v.Relevance = nullableInt(relevance)
		v.ApplicationID = nullableInt(appID)
		v.PublishedAt = nullableText(published)
		v.LoadedAt = nullableText(loaded)
		v.LastCheckedAt = nullableText(checked)
		v.Favorite = favorite != 0
		v.Hidden = hidden != 0
		v.Archived = archived != 0
		if v.EntityType != "agency" && v.EntityType != "channel" {
			v.EntityType = "company"
		}
		v.CoverLetterScope = "shared"
		v.Roles = []roles.Role{}
		v.TelegramSources = []DetailTelegramSource{}
		index[v.ID] = len(result)
		result = append(result, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(result) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(result)), ",")
		ids := make([]any, len(result))
		for i, v := range result {
			ids[i] = v.ID
		}
		// Role assignments do not contain personal information.
		rows, err = tx.QueryContext(ctx, `SELECT vacancy_id,role_id FROM vacancy_role_assignments WHERE vacancy_id IN (`+placeholders+`) ORDER BY vacancy_id,role_id`, ids...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var vacancyID, roleID int64
			if err = rows.Scan(&vacancyID, &roleID); err != nil {
				break
			}
			if role, ok := knownRoles[roleID]; ok {
				result[index[vacancyID]].Roles = append(result[index[vacancyID]].Roles, role)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		for i := range result {
			v := &result[i]
			sort.Slice(v.Roles, func(a, b int) bool { return v.Roles[a].Path < v.Roles[b].Path })
			if len(v.Roles) > 0 {
				names := make([]string, 0, len(v.Roles))
				for _, role := range v.Roles {
					names = append(names, role.Name)
				}
				v.Role = strings.Join(names, ", ")
			}
		}
		rows, err = tx.QueryContext(ctx, `SELECT s.vacancy_id,s.channel_id,COALESCE(c.name,''),COALESCE(c.url,''),COALESCE(c.username,''),COALESCE(s.message_url,''),COALESCE(s.telegram_contact,'')
            FROM vacancy_telegram_sources s JOIN telegram_channels c ON c.id=s.channel_id
            WHERE s.vacancy_id IN (`+placeholders+`) ORDER BY s.id`, ids...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var vacancyID int64
			var source DetailTelegramSource
			if err = rows.Scan(&vacancyID, &source.ChannelID, &source.ChannelName, &source.ChannelURL, &source.ChannelUsername, &source.MessageURL, &source.TelegramContact); err != nil {
				break
			}
			if source.ChannelID > 0 && source.MessageURL != "" {
				result[index[vacancyID]].TelegramSources = append(result[index[vacancyID]].TelegramSources, source)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}

		// One personal letter per user and vacancy, newest revision wins.
		letterArgs := make([]any, 0, len(ids)+1)
		letterArgs = append(letterArgs, user)
		letterArgs = append(letterArgs, ids...)
		rows, err = tx.QueryContext(ctx, `SELECT vacancy_id,subject,markdown,override_shared FROM vacancy_cover_letters
            WHERE user_name=? AND vacancy_id IN (`+placeholders+`) ORDER BY revision DESC,created_at DESC`, letterArgs...)
		if err != nil {
			return nil, err
		}
		seen := map[int64]bool{}
		for rows.Next() {
			var vacancyID int64
			var subject, markdown sql.NullString
			var override sql.NullInt64
			if err = rows.Scan(&vacancyID, &subject, &markdown, &override); err != nil {
				break
			}
			if seen[vacancyID] {
				continue
			}
			seen[vacancyID] = true
			i, ok := index[vacancyID]
			if !ok {
				err = fmt.Errorf("unmatched vacancy cover letter: %d", vacancyID)
				break
			}
			v := &result[i]
			v.CoverLetterSubject, v.CoverLetterMD, v.CoverLetterScope = EffectiveLetter(v.CoverLetterSubject, v.CoverLetterMD, subject, markdown, override.Int64 != 0, true)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
