package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"itmap/internal/roles"
)

// ListResponse mirrors /api/vacancies/list in Sites 316. It deliberately
// excludes vacancy descriptions, cover letters and private application notes.
type ListResponse struct {
	Vacancies          []Vacancy            `json:"vacancies"`
	Entities           []Entity             `json:"entities"`
	RoleTree           []roles.Role         `json:"roleTree"`
	ApplicationSummary []ApplicationSummary `json:"applicationSummary"`
}

type Vacancy struct {
	ID                int64            `json:"id"`
	CompanyName       string           `json:"companyName"`
	EntityType        string           `json:"entityType"`
	RealCompanyName   string           `json:"realCompanyName"`
	Title             string           `json:"title"`
	SalaryFrom        *int64           `json:"salaryFrom"`
	SalaryTo          *int64           `json:"salaryTo"`
	Role              string           `json:"role"`
	Level             string           `json:"level"`
	Relevance         *int64           `json:"relevance"`
	Favorite          bool             `json:"favorite"`
	Hidden            bool             `json:"hidden"`
	PublishedAt       *string          `json:"publishedAt"`
	LoadedAt          *string          `json:"loadedAt"`
	Archived          bool             `json:"archived"`
	CreatedAt         string           `json:"createdAt"`
	ApplicationID     *int64           `json:"applicationId"`
	ApplicationStatus string           `json:"applicationStatus"`
	TelegramSources   []TelegramSource `json:"telegramSources"`
	Roles             []roles.Role     `json:"roles"`
}

type TelegramSource struct {
	ChannelID   int64  `json:"channelId"`
	ChannelName string `json:"channelName"`
}

type Entity struct {
	Name            string `json:"name"`
	Rank            any    `json:"rank"`
	EntityType      string `json:"entityType"`
	Favorite        bool   `json:"favorite"`
	Hidden          bool   `json:"hidden"`
	HasVacancies    bool   `json:"hasVacancies"`
	HasApplications bool   `json:"hasApplications"`
	Middle          any    `json:"middle"`
	Senior          any    `json:"senior"`
	Lead            any    `json:"lead"`
	Head            any    `json:"head"`
	ApplicationURL  any    `json:"applicationUrl"`
}

type ApplicationSummary struct {
	CompanyName string `json:"companyName"`
	VacancyID   *int64 `json:"vacancyId"`
}

// VacancyState is one legacy result row, without user-visible JSON details.
type VacancyState struct {
	Vacancy
	SalaryFromSQL, SalaryToSQL, RelevanceSQL, ApplicationIDSQL sql.NullInt64
	PublishedSQL, LoadedSQL, RealCompanySQL                    sql.NullString
	FavoriteSQL, HiddenSQL, ArchivedSQL                        int64
}

func (s VacancyState) project() Vacancy {
	v := s.Vacancy
	v.SalaryFrom = nullableInt(s.SalaryFromSQL)
	v.SalaryTo = nullableInt(s.SalaryToSQL)
	v.Relevance = nullableInt(s.RelevanceSQL)
	v.ApplicationID = nullableInt(s.ApplicationIDSQL)
	v.PublishedAt = nullableText(s.PublishedSQL)
	v.LoadedAt = nullableText(s.LoadedSQL)
	v.RealCompanyName = s.RealCompanySQL.String
	v.Favorite = s.FavoriteSQL != 0
	v.Hidden = s.HiddenSQL != 0
	v.Archived = s.ArchivedSQL != 0
	if v.EntityType != "agency" && v.EntityType != "channel" {
		v.EntityType = "company"
	}
	v.TelegramSources = []TelegramSource{}
	v.Roles = []roles.Role{}
	return v
}

func nullableInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
func nullableText(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

// BuildList only projects read-side records; no lazy migration or INSERT is
// performed on GET. The original Sites prepareVacancyProjection writes data.
func BuildList(input []Vacancy, selected map[int64][]roles.Role, channels map[int64][]TelegramSource, catalog []map[string]any, companyFlags map[string]entityFlags, roleTree []roles.Role, general map[string]bool, period *Period, now time.Time) ListResponse {
	out := ListResponse{Vacancies: []Vacancy{}, Entities: []Entity{}, RoleTree: roleTree, ApplicationSummary: []ApplicationSummary{}}
	if out.RoleTree == nil {
		out.RoleTree = []roles.Role{}
	}
	wanted := map[string]bool{}
	for _, item := range input {
		item.Roles = roles.OrderAssigned(selected[item.ID])
		if len(item.Roles) > 0 {
			names := make([]string, 0, len(item.Roles))
			for _, r := range item.Roles {
				names = append(names, r.Name)
			}
			item.Role = strings.Join(names, ", ")
		}
		item.TelegramSources = append([]TelegramSource{}, channels[item.ID]...)
		wanted[item.CompanyName] = true
		if item.RealCompanyName != "" {
			wanted[item.RealCompanyName] = true
		}
		if period == nil || MatchesPublication(*period, Dates{PublishedAt: deref(item.PublishedAt), LoadedAt: deref(item.LoadedAt), CreatedAt: item.CreatedAt}, now) {
			out.Vacancies = append(out.Vacancies, item)
		}
	}
	// Entities and general application summary are determined from the full
	// vacancy list (before period filtering), exactly as in legacy v316.
	for _, entry := range catalog {
		name, _ := entry["name"].(string)
		if !wanted[name] {
			continue
		}
		flags := companyFlags[name]
		typ, _ := entry["entityType"].(string)
		if typ != "agency" {
			typ = "company"
		}
		out.Entities = append(out.Entities, Entity{
			Name: name, Rank: entry["rank"], EntityType: typ, Favorite: flags.favorite, Hidden: flags.hidden,
			HasVacancies: true, HasApplications: flags.applications,
			Middle: entry["middle"], Senior: entry["senior"], Lead: entry["lead"], Head: entry["head"], ApplicationURL: entry["applicationUrl"],
		})
	}
	// Original applicationSummary represents company-wide applications only.
	names := make([]string, 0, len(general))
	for name, exists := range general {
		if exists && wanted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out.ApplicationSummary = append(out.ApplicationSummary, ApplicationSummary{CompanyName: name})
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type entityFlags struct{ favorite, hidden, applications bool }

type Repository struct{ DB *sql.DB }

// List is a read-only snapshot. Rows are scoped to the authenticated user;
// a missing user must never default to the administrator.
func (r Repository) List(ctx context.Context, user string, period *Period, now time.Time) (ListResponse, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return ListResponse{}, errors.New("vacancy DB/user unavailable")
	}
	if period != nil {
		if _, err := ParsePeriod(string(*period)); err != nil {
			return ListResponse{}, err
		}
	}
	roleCatalog, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return ListResponse{}, fmt.Errorf("read role catalog: %w", err)
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ListResponse{}, err
	}
	defer tx.Rollback()
	var catalogJSON []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalogJSON); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ListResponse{}, err
	}

	// Window functions avoid accidental multiplication by duplicate user-state
	// rows in the direct D1 -> MariaDB dump (no composite unique constraint).
	const query = `WITH latest_app AS (
        SELECT id,vacancy_id,status,ROW_NUMBER() OVER(PARTITION BY vacancy_id ORDER BY updated_at DESC,id DESC) AS rn
        FROM company_applications WHERE user_name=? AND vacancy_id IS NOT NULL
    ), latest_state AS (
        SELECT vacancy_id,relevance,favorite,hidden,ROW_NUMBER() OVER(
            PARTITION BY vacancy_id ORDER BY updated_at DESC,favorite DESC,hidden DESC,relevance DESC) AS rn
        FROM company_vacancy_user_state WHERE user_name=?
    )
    SELECT v.id,v.company_name,v.entity_type,v.real_company_name,v.title,
      v.salary_from,v.salary_to,v.role,v.level,s.relevance,
      COALESCE(s.favorite,0),COALESCE(s.hidden,0),v.published_at,v.loaded_at,
      v.archived,v.created_at,a.id,COALESCE(a.status,'')
    FROM company_vacancies v
    LEFT JOIN latest_state s ON s.vacancy_id=v.id AND s.rn=1
    LEFT JOIN latest_app a ON a.vacancy_id=v.id AND a.rn=1
    ORDER BY v.company_name,v.published_at IS NULL,v.published_at DESC,v.id DESC`
	rows, err := tx.QueryContext(ctx, query, user, user)
	if err != nil {
		return ListResponse{}, err
	}
	all := make([]Vacancy, 0)
	for rows.Next() {
		var s VacancyState
		err = rows.Scan(&s.ID, &s.CompanyName, &s.EntityType, &s.RealCompanySQL, &s.Title,
			&s.SalaryFromSQL, &s.SalaryToSQL, &s.Role, &s.Level, &s.RelevanceSQL,
			&s.FavoriteSQL, &s.HiddenSQL, &s.PublishedSQL, &s.LoadedSQL, &s.ArchivedSQL,
			&s.CreatedAt, &s.ApplicationIDSQL, &s.ApplicationStatus)
		if err != nil {
			break
		}
		all = append(all, s.project())
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ListResponse{}, err
	}
	assigned := make(map[int64][]roles.Role)
	byID := make(map[int64]roles.Role, len(roleCatalog.Roles))
	for _, role := range roleCatalog.Roles {
		byID[role.ID] = role
	}
	rows, err = tx.QueryContext(ctx, "SELECT vacancy_id,role_id FROM vacancy_role_assignments ORDER BY vacancy_id,role_id")
	if err != nil {
		return ListResponse{}, err
	}
	for rows.Next() {
		var vacancyID, roleID int64
		err = rows.Scan(&vacancyID, &roleID)
		if err != nil {
			break
		}
		if role, ok := byID[roleID]; ok {
			assigned[vacancyID] = append(assigned[vacancyID], role)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ListResponse{}, err
	}
	sources := map[int64][]TelegramSource{}
	rows, err = tx.QueryContext(ctx, `SELECT s.vacancy_id,s.channel_id,c.name FROM vacancy_telegram_sources s JOIN telegram_channels c ON c.id=s.channel_id ORDER BY s.vacancy_id,s.id`)
	if err != nil {
		return ListResponse{}, err
	}
	for rows.Next() {
		var id int64
		var source TelegramSource
		err = rows.Scan(&id, &source.ChannelID, &source.ChannelName)
		if err != nil {
			break
		}
		sources[id] = append(sources[id], source)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ListResponse{}, err
	}
	flags := map[string]entityFlags{}
	rows, err = tx.QueryContext(ctx, "SELECT company_name,favorite,hidden FROM company_user_state WHERE user_name=? ORDER BY updated_at", user)
	if err != nil {
		return ListResponse{}, err
	}
	for rows.Next() {
		var name string
		var favorite, hidden int64
		err = rows.Scan(&name, &favorite, &hidden)
		if err != nil {
			break
		}
		f := flags[name]
		f.favorite = favorite != 0
		f.hidden = hidden != 0
		flags[name] = f
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ListResponse{}, err
	}
	general := map[string]bool{}
	rows, err = tx.QueryContext(ctx, `SELECT company_name,MAX(vacancy_id IS NULL) FROM company_applications WHERE user_name=? GROUP BY company_name`, user)
	if err != nil {
		return ListResponse{}, err
	}
	for rows.Next() {
		var name string
		var hasGeneral int64
		err = rows.Scan(&name, &hasGeneral)
		if err != nil {
			break
		}
		general[name] = hasGeneral != 0
		f := flags[name]
		f.applications = true
		flags[name] = f
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ListResponse{}, err
	}
	names := make([]string, 0, len(all)*2)
	for _, vacancy := range all {
		names = append(names, vacancy.CompanyName, vacancy.RealCompanyName)
	}
	entries, err := vacancyCatalogEntries(catalogJSON, names)
	if err != nil {
		return ListResponse{}, err
	}
	result := BuildList(all, assigned, sources, entries, flags, roleCatalog.Roles, general, period, now)
	if err = tx.Commit(); err != nil {
		return ListResponse{}, err
	}
	return result, nil
}
