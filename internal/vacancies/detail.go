package vacancies

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"itmap/internal/roles"
)

// Detail reproduces the v316 vacancy drawer contract. Full descriptions and
// private cover letters are never returned by the compact list endpoint.
type Detail struct {
	Vacancy
	URL                string                 `json:"url"`
	DescriptionMD      string                 `json:"descriptionMd"`
	CoverLetterSubject string                 `json:"coverLetterSubject"`
	CoverLetterMD      string                 `json:"coverLetterMd"`
	CoverLetterScope   string                 `json:"coverLetterScope"`
	LastCheckedAt      *string                `json:"lastCheckedAt"`
	UpdatedAt          string                 `json:"updatedAt"`
	Revision           string                 `json:"revision"`
	TelegramSources    []DetailTelegramSource `json:"telegramSources"`
	PublicationSources []PublicationSource    `json:"publicationSources"`
}

type DetailTelegramSource struct {
	ChannelID       int64  `json:"channelId"`
	ChannelName     string `json:"channelName"`
	ChannelURL      string `json:"channelUrl"`
	ChannelUsername string `json:"channelUsername"`
	MessageURL      string `json:"messageUrl"`
	TelegramContact string `json:"telegramContact"`
	// Internal provenance contributes to the revision, not the response.
	id, createdAt, updatedAt string
}

type PublicationSource struct {
	CandidateID int64  `json:"candidateId"`
	URL         string `json:"url"`
	UpdatedAt   string `json:"updatedAt"`
}

type DetailResponse struct {
	Vacancy  Detail   `json:"vacancy"`
	Entities []Entity `json:"entities"`
}

var ErrNotFound = errors.New("vacancy not found")

// EffectiveLetter exactly follows the old shared/personal fallback rules.
func EffectiveLetter(sharedSubject, sharedMD string, personalSubject, personalMD sql.NullString, override bool, hasPersonal bool) (string, string, string) {
	if !hasPersonal {
		return sharedSubject, sharedMD, "shared"
	}
	if override {
		return personalSubject.String, personalMD.String, "personal"
	}
	if strings.TrimSpace(sharedMD) != "" {
		return sharedSubject, sharedMD, "shared"
	}
	return personalSubject.String, personalMD.String, "personal"
}

// DetailRevision is a Go-side optimistic-lock token, intentionally NOT a
// promise of byte-for-byte SQLite json_array hashes from the legacy runtime.
// When PUT is migrated it must validate this same token inside its transaction.
func DetailRevision(v Detail, snapshot any) (string, error) {
	// Never include revision itself in its hash.
	v.Revision = ""
	// Display-only/user-state properties are excluded from editing revisions.
	// The source of truth for assigned roles, letters and Telegram rows is the
	// explicit snapshot supplied by the repository, not display projections.
	v.Roles = nil
	v.TelegramSources = nil
	v.PublicationSources = nil
	v.Role = ""
	v.Favorite = false
	v.Hidden = false
	v.ApplicationID = nil
	v.ApplicationStatus = ""
	v.CoverLetterSubject = ""
	v.CoverLetterMD = ""
	v.CoverLetterScope = ""
	bytes, err := json.Marshal(struct {
		Vacancy  Detail `json:"vacancy"`
		Snapshot any    `json:"state"`
	}{v, snapshot})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(bytes)
	return hex.EncodeToString(digest[:]), nil
}

func telegramRevisionRows(sources []DetailTelegramSource) []string {
	result := make([]string, 0, len(sources))
	for _, s := range sources {
		result = append(result, strings.Join([]string{s.id, fmt.Sprint(s.ChannelID), s.MessageURL, s.TelegramContact, s.createdAt, s.updatedAt}, "\x00"))
	}
	return result
}

func detailEntities(data []byte, company, realCompany string) ([]Entity, error) {
	var catalog struct{ Companies, Agencies []map[string]any }
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	if catalog.Companies == nil || catalog.Agencies == nil {
		return nil, errors.New("incomplete catalog")
	}
	out := make([]Entity, 0, 2)
	for _, set := range []struct {
		entries []map[string]any
		kind    string
	}{{catalog.Companies, "company"}, {catalog.Agencies, "agency"}} {
		for _, c := range set.entries {
			name, _ := c["name"].(string)
			if name != company && name != realCompany {
				continue
			}
			kind := set.kind
			if v, _ := c["entityType"].(string); v == "agency" {
				kind = "agency"
			}
			out = append(out, Entity{Name: name, Rank: c["rank"], EntityType: kind,
				Middle: c["middle"], Senior: c["senior"], Lead: c["lead"], Head: c["head"], ApplicationURL: c["applicationUrl"]})
		}
	}
	return out, nil
}

// DetailByID obtains a consistent read-only snapshot of one vacancy and its
// user-scoped state. Unlike Sites readVacancyDetail it never initializes data
// or writes to the database on GET.
func (r Repository) DetailByID(ctx context.Context, user string, id int64) (DetailResponse, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id < 1 {
		return DetailResponse{}, errors.New("vacancy DB, user or id unavailable")
	}
	roleCatalog, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return DetailResponse{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return DetailResponse{}, err
	}
	defer tx.Rollback()
	// The converted MariaDB tables do not all have their old SQLite unique
	// constraints. Correlated subqueries choose one deterministically without
	// multiplying vacancy rows when old duplicate state records exist.
	const q = `SELECT v.id,v.company_name,v.entity_type,v.real_company_name,v.title,
      v.salary_from,v.salary_to,v.role,v.level,
      (SELECT s.relevance FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),
      COALESCE((SELECT s.favorite FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),0),
      COALESCE((SELECT s.hidden FROM company_vacancy_user_state s WHERE s.user_name=? AND s.vacancy_id=v.id ORDER BY s.updated_at DESC,s.favorite DESC,s.hidden DESC,s.relevance DESC LIMIT 1),0),
      v.published_at,v.loaded_at,v.archived,v.created_at,
      (SELECT a.id FROM company_applications a WHERE a.user_name=? AND a.vacancy_id=v.id ORDER BY a.updated_at DESC,a.id DESC LIMIT 1),
      COALESCE((SELECT a.status FROM company_applications a WHERE a.user_name=? AND a.vacancy_id=v.id ORDER BY a.updated_at DESC,a.id DESC LIMIT 1),''),
      v.url,v.description_md,v.cover_letter_subject,v.cover_letter_md,
      v.last_checked_at,v.updated_at
      FROM company_vacancies v WHERE v.id=?`
	var s VacancyState
	var v Detail
	var sharedSubject, sharedMD string
	err = tx.QueryRowContext(ctx, q, user, user, user, user, user, id).Scan(
		&s.ID, &s.CompanyName, &s.EntityType, &s.RealCompanySQL, &s.Title,
		&s.SalaryFromSQL, &s.SalaryToSQL, &s.Role, &s.Level, &s.RelevanceSQL,
		&s.FavoriteSQL, &s.HiddenSQL, &s.PublishedSQL, &s.LoadedSQL, &s.ArchivedSQL,
		&s.CreatedAt, &s.ApplicationIDSQL, &s.ApplicationStatus,
		&v.URL, &v.DescriptionMD, &sharedSubject, &sharedMD, &v.LastCheckedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DetailResponse{}, ErrNotFound
	}
	if err != nil {
		return DetailResponse{}, err
	}
	v.Vacancy = s.project()

	// A personal letter has its own revision; it is not shared with other users.
	var personalSubject, personalMD sql.NullString
	var override sql.NullInt64
	var letterRevision sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT subject,markdown,override_shared,revision FROM vacancy_cover_letters
        WHERE user_name=? AND vacancy_id=? ORDER BY revision DESC,created_at DESC LIMIT 1`, user, id).
		Scan(&personalSubject, &personalMD, &override, &letterRevision)
	hasPersonal := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DetailResponse{}, err
	}
	v.CoverLetterSubject, v.CoverLetterMD, v.CoverLetterScope = EffectiveLetter(sharedSubject, sharedMD, personalSubject, personalMD, override.Int64 != 0, hasPersonal)

	var roleIDs []int64
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=? ORDER BY role_id", id)
	if err != nil {
		return DetailResponse{}, err
	}
	for rows.Next() {
		var roleID int64
		err = rows.Scan(&roleID)
		if err != nil {
			break
		}
		roleIDs = append(roleIDs, roleID)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return DetailResponse{}, err
	}
	byID := make(map[int64]roles.Role, len(roleCatalog.Roles))
	for _, role := range roleCatalog.Roles {
		byID[role.ID] = role
	}
	for _, roleID := range roleIDs {
		if role, ok := byID[roleID]; ok {
			v.Roles = append(v.Roles, role)
		}
	}
	sort.Slice(v.Roles, func(i, j int) bool { return v.Roles[i].Path < v.Roles[j].Path })
	if len(v.Roles) > 0 {
		names := make([]string, 0, len(v.Roles))
		for _, role := range v.Roles {
			names = append(names, role.Name)
		}
		v.Role = strings.Join(names, ", ")
	}

	v.TelegramSources = []DetailTelegramSource{}
	rows, err = tx.QueryContext(ctx, `SELECT s.id,s.channel_id,c.name,c.url,c.username,s.message_url,s.telegram_contact,s.created_at,s.updated_at
        FROM vacancy_telegram_sources s JOIN telegram_channels c ON c.id=s.channel_id WHERE s.vacancy_id=? ORDER BY s.id`, id)
	if err != nil {
		return DetailResponse{}, err
	}
	for rows.Next() {
		var src DetailTelegramSource
		var rowID int64
		err = rows.Scan(&rowID, &src.ChannelID, &src.ChannelName, &src.ChannelURL, &src.ChannelUsername, &src.MessageURL, &src.TelegramContact, &src.createdAt, &src.updatedAt)
		if err != nil {
			break
		}
		src.id = fmt.Sprint(rowID)
		if src.ChannelID > 0 && src.MessageURL != "" {
			v.TelegramSources = append(v.TelegramSources, src)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return DetailResponse{}, err
	}
	v.PublicationSources = []PublicationSource{}
	rows, err = tx.QueryContext(ctx, "SELECT candidate_id,url,updated_at FROM vacancy_publication_sources WHERE vacancy_id=? ORDER BY candidate_id", id)
	if err != nil {
		return DetailResponse{}, err
	}
	for rows.Next() {
		var p PublicationSource
		err = rows.Scan(&p.CandidateID, &p.URL, &p.UpdatedAt)
		if err != nil {
			break
		}
		v.PublicationSources = append(v.PublicationSources, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return DetailResponse{}, err
	}
	var catalogJSON []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalogJSON); err != nil {
		return DetailResponse{}, err
	}
	entities, err := detailEntities(catalogJSON, v.CompanyName, v.RealCompanyName)
	if err != nil {
		return DetailResponse{}, err
	}
	// A revision must change when any underlying shared state or the personal
	// letter changes, even when the displayed effective letter does not.
	v.Revision, err = DetailRevision(v, struct {
		SharedSubject  string   `json:"sharedSubject"`
		SharedMD       string   `json:"sharedMd"`
		LetterRevision *int64   `json:"letterRevision"`
		LetterOverride int64    `json:"letterOverride"`
		LetterSubject  string   `json:"letterSubject"`
		LetterMD       string   `json:"letterMd"`
		RoleIDs        []int64  `json:"roleIds"`
		TelegramRows   []string `json:"telegramRows"`
	}{sharedSubject, sharedMD, nullableInt(letterRevision), override.Int64, personalSubject.String, personalMD.String, roleIDs, telegramRevisionRows(v.TelegramSources)})
	if err != nil {
		return DetailResponse{}, err
	}
	if err = tx.Commit(); err != nil {
		return DetailResponse{}, err
	}
	return DetailResponse{Vacancy: v, Entities: entities}, nil
}
