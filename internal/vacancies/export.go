package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"itmap/internal/roles"
	"itmap/internal/xlsx"
)

var exportHeaders = []string{
	"ID компании", "Название компании", "Название вакансии", "Ссылка на вакансию",
	"Зарплата от", "Зарплата до", "Уровень вакансии", "Роль", "Релевантность",
	"Дата публикации", "Дата загрузки", "Текст вакансии (Markdown)",
	"Тип владельца", "ID агентства", "Название агентства", "ID реального работодателя",
	"Реальный работодатель", "Заголовок сопроводительного письма", "Текст сопроводительного письма (Markdown)",
}

const exportQuery = `WITH latest_state AS (
	SELECT vacancy_id,relevance,ROW_NUMBER() OVER(
		PARTITION BY vacancy_id ORDER BY updated_at DESC,favorite DESC,hidden DESC,relevance DESC) AS rn
	FROM company_vacancy_user_state WHERE user_name=?
)
SELECT v.id, owner_identity.id, v.company_name, v.title, v.url, v.salary_from, v.salary_to, v.level, v.role,
	s.relevance, v.published_at, v.loaded_at, v.description_md, v.entity_type, agency.id,
	employer.id, v.real_company_name, v.cover_letter_subject, v.cover_letter_md
FROM company_vacancies v
LEFT JOIN latest_state s ON s.vacancy_id=v.id AND s.rn=1
LEFT JOIN company_identity owner_identity ON owner_identity.company_name=v.company_name AND v.entity_type='company'
LEFT JOIN agency_identity agency ON agency.agency_name=v.company_name AND v.entity_type='agency'
LEFT JOIN company_identity employer ON employer.company_name=v.real_company_name
ORDER BY BINARY v.entity_type, COALESCE(owner_identity.id, agency.id), v.published_at IS NULL, BINARY v.published_at DESC, v.id DESC`

func (r Repository) ExportXLSX(ctx context.Context, user string) ([]byte, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return nil, errors.New("vacancy export storage or user unavailable")
	}
	catalog, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return nil, err
	}
	paths := make(map[int64]string, len(catalog.Roles))
	for _, role := range catalog.Roles {
		paths[role.ID] = role.Path
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	assignments, err := tx.QueryContext(ctx, "SELECT vacancy_id,role_id FROM vacancy_role_assignments")
	if err != nil {
		return nil, err
	}
	assigned := map[int64][]string{}
	for assignments.Next() {
		var vacancyID, roleID int64
		if err = assignments.Scan(&vacancyID, &roleID); err != nil {
			assignments.Close()
			return nil, err
		}
		if path, ok := paths[roleID]; ok {
			assigned[vacancyID] = append(assigned[vacancyID], path)
		}
	}
	assignments.Close()
	if err = assignments.Err(); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, exportQuery, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	book, err := xlsx.New("Вакансии")
	if err != nil {
		return nil, err
	}
	book.Sheet(exportHeaders, vacancyExportColumns)
	for rows.Next() {
		var id int64
		var companyID, agencyID, realID, salaryFrom, salaryTo, relevance sql.NullInt64
		var published, loaded, realName sql.NullString
		var owner, title, url, level, role, description, entityType, letterSubject, letter string
		err = rows.Scan(&id, &companyID, &owner, &title, &url, &salaryFrom, &salaryTo, &level, &role,
			&relevance, &published, &loaded, &description, &entityType, &agencyID, &realID, &realName, &letterSubject, &letter)
		if err != nil {
			return nil, err
		}
		if values := assigned[id]; len(values) > 0 {
			sort.Strings(values)
			role = strings.Join(values, "; ")
		}
		company, agency, ownerType := "", "", "Компания"
		switch entityType {
		case "company":
			company = owner
		case "agency":
			agency, ownerType = owner, "Агентство"
		}
		book.Row(companyID, company, title, url, salaryFrom, salaryTo, level, role, relevance,
			published.String, loaded.String, description, ownerType, agencyID, agency, realID, realName.String, letterSubject, letter)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return book.Finish()
}

const vacancyExportColumns = `<cols><col min="1" max="3" width="28" customWidth="1"/><col min="4" max="4" width="55" customWidth="1"/><col min="5" max="11" width="22" customWidth="1"/><col min="12" max="12" width="85" customWidth="1"/><col min="13" max="19" width="28" customWidth="1"/></cols>`
