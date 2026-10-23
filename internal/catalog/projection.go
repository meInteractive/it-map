package catalog

// Compact catalog projections preserve the v316 response shape while avoiding
// the legacy GET-time INSERT OR IGNORE writes. IDs must come from an already
// migrated identity table: MariaDB's initial D1 conversion does not enforce
// uniqueness of the LONGTEXT identity names.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type EntityType string

const (
	CompanyType EntityType = "company"
	AgencyType  EntityType = "agency"
)

type View string

const (
	ListView View = "list"
	MapView  View = "map"
)

// CompactRepository is intentionally narrow so the HTTP layer cannot perform
// arbitrary writes or inspect user-private records other than projections.
type CompactRepository interface {
	ListEntities(context.Context, string, EntityType, View) ([]map[string]any, error)
	EntityDetail(context.Context, string, EntityType, int64, string) (map[string]any, error)
}

var ErrNotFound = errors.New("catalog entity not found")

type storedEntity map[string]any
type sourceCatalog struct {
	Companies []storedEntity `json:"companies"`
	Agencies  []storedEntity `json:"agencies"`
}

type entityState struct {
	ID               int64
	Favorite, Hidden bool
}
type applicationState struct{ Status string }
type catalogState struct {
	Identities   map[string]entityState
	Vacancies    map[string]bool
	Applications map[string]applicationState
	Initialized  map[string]bool
}

func entityKey(kind EntityType, name string) string { return string(kind) + "\x00" + name }

func asText(v any) string { s, _ := v.(string); return s }
func asSlice(v any) []any { a, _ := v.([]any); return a }
func optional(v any) any {
	if v == nil {
		return nil
	}
	return v
}
func salary(v any) any {
	if a, ok := v.([]any); ok && len(a) == 2 {
		return a
	}
	return []any{nil, nil}
}
func isSet(v any) bool             { return strings.TrimSpace(asText(v)) != "" }
func obj(v any) map[string]any     { m, _ := v.(map[string]any); return m }
func nameOf(e storedEntity) string { return asText(e["name"]) }
func parseCatalog(data []byte) (sourceCatalog, error) {
	var c sourceCatalog
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("decode app_catalog: %w", err)
	}
	if c.Companies == nil || c.Agencies == nil {
		return c, errors.New("catalog missing companies or agencies")
	}
	return c, nil
}
func (c sourceCatalog) entities(kind EntityType) []storedEntity {
	if kind == AgencyType {
		return c.Agencies
	}
	return c.Companies
}

// projectCompact maps only fields used by list/map, never exposes private CRM
// comments, vacancies' descriptions, resumes or recruiter contacts.
func projectCompact(e storedEntity, kind EntityType, view View, state catalogState) map[string]any {
	name := nameOf(e)
	s, found := state.Identities[entityKey(kind, name)]
	var id any
	if found {
		id = s.ID
	}
	jobsPresent := false
	if !state.Initialized[name] {
		for _, v := range asSlice(e["jobs"]) {
			j := obj(v)
			u := asText(j["url"])
			if isSet(j["title"]) && (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
				jobsPresent = true
				break
			}
		}
	}
	base := map[string]any{
		"id": id, "name": name, "rank": optional(e["rank"]), "entityType": string(kind),
		"favorite": s.Favorite, "hidden": s.Hidden,
		"hasVacancies":    state.Vacancies[name] || jobsPresent,
		"hasApplications": false, "applicationStatus": nil,
	}
	if a, ok := state.Applications[name]; ok {
		base["hasApplications"] = true
		if a.Status != "" {
			base["applicationStatus"] = a.Status
		}
	}
	offices := make([]map[string]any, 0)
	for _, v := range asSlice(e["offices"]) {
		o := obj(v)
		item := map[string]any{"address": optional(o["address"]), "metro": optional(o["metro"])}
		if view == MapView {
			item["lat"] = optional(o["lat"])
			item["lon"] = optional(o["lon"])
		}
		offices = append(offices, item)
	}
	base["offices"] = offices
	if view == MapView {
		return base
	}
	for _, k := range []string{"middle", "senior", "lead", "head"} {
		base[k] = salary(e[k])
	}
	base["deferment"] = e["deferment"]
	if base["deferment"] == nil {
		base["deferment"] = ""
	}
	base["rating"] = optional(e["rating"])
	base["wlbScore"] = optional(e["wlbScore"])
	sources := obj(e["vacancySources"])
	foundSource := false
	for _, v := range sources {
		if isSet(v) {
			foundSource = true
			break
		}
	}
	base["hasVacancySource"] = foundSource
	if kind == AgencyType {
		base["reviewsScore"] = optional(e["reviewsScore"])
		base["salaryRangeSource"] = optional(e["salaryRangeSource"])
		base["revenue"] = map[string]any{"amount": optional(obj(e["revenue"])["amount"])}
		base["functionalSpecializations"] = optional(e["functionalSpecializations"])
	}
	return base
}

func projectList(c sourceCatalog, kind EntityType, view View, s catalogState) []map[string]any {
	entities := c.entities(kind)
	result := make([]map[string]any, 0, len(entities))
	for _, e := range entities {
		result = append(result, projectCompact(e, kind, view, s))
	}
	return result
}

// projectDetail follows the legacy readCompanyDetail: exclude embedded jobs,
// append state/ID/count, and retain other source fields unmodified.
func projectDetail(e storedEntity, kind EntityType, id any, favorite, hidden bool, count int64) map[string]any {
	result := make(map[string]any, len(e)+4)
	for k, v := range e {
		if k != "jobs" {
			result[k] = v
		}
	}
	result["entityType"] = string(kind)
	result["id"] = id
	result["favorite"] = favorite
	result["hidden"] = hidden
	result["vacancyCount"] = count
	return result
}

func (r Repository) ListEntities(ctx context.Context, user string, kind EntityType, view View) ([]map[string]any, error) {
	if r.DB == nil {
		return nil, errors.New("catalog database not configured")
	}
	if kind != CompanyType && kind != AgencyType {
		return nil, errors.New("unsupported entity type")
	}
	if view != ListView && view != MapView {
		return nil, errors.New("unsupported projection")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var data []byte
	if err := tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil {
		return nil, err
	}
	c, err := parseCatalog(data)
	if err != nil {
		return nil, err
	}
	state, err := loadCatalogState(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	result := projectList(c, kind, view, state)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// loadCatalogState uses read-only queries; unlike the old Cloudflare code,
// identity rows are not inserted on every page render. Existing IDs remain stable.
func loadCatalogState(ctx context.Context, tx *sql.Tx, user string) (catalogState, error) {
	s := catalogState{Identities: map[string]entityState{}, Vacancies: map[string]bool{}, Applications: map[string]applicationState{}, Initialized: map[string]bool{}}
	for _, source := range []struct {
		table, column string
		kind          EntityType
	}{{"company_identity", "company_name", CompanyType}, {"agency_identity", "agency_name", AgencyType}} {
		// Whitelisted identifiers only, no interpolation of untrusted values.
		rows, err := tx.QueryContext(ctx, "SELECT id, "+source.column+" FROM "+source.table)
		if err != nil {
			return s, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err = rows.Scan(&id, &name); err != nil {
				break
			}
			key := entityKey(source.kind, name)
			if old, ok := s.Identities[key]; !ok || id < old.ID {
				s.Identities[key] = entityState{ID: id}
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return s, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT company_name, favorite, hidden FROM company_user_state WHERE user_name=?", user)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var name string
		var favorite, hidden int64
		if err = rows.Scan(&name, &favorite, &hidden); err != nil {
			break
		}
		for _, kind := range []EntityType{CompanyType, AgencyType} {
			key := entityKey(kind, name)
			if v, ok := s.Identities[key]; ok {
				v.Favorite = favorite != 0
				v.Hidden = hidden != 0
				s.Identities[key] = v
			}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT company_name FROM company_vacancies UNION SELECT real_company_name FROM company_vacancies WHERE real_company_name IS NOT NULL AND real_company_name<>''")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			break
		}
		s.Vacancies[n] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return s, err
	}
	// The legacy rule prefers applications tied to a vacancy, then the status
	// furthest along the application funnel ("Отказ" sorts below all others).
	rows, err = tx.QueryContext(ctx, `SELECT company_name,status FROM company_applications WHERE user_name=?
  ORDER BY company_name,(vacancy_id IS NOT NULL) DESC,
  CASE status WHEN 'Отказ' THEN 0 WHEN '' THEN -1 WHEN 'Откликнулся' THEN 1
   WHEN 'Получил ответ' THEN 2 WHEN 'Скрининг' THEN 3 WHEN 'Собес 2' THEN 4
   WHEN 'Собес 3' THEN 5 WHEN 'Собес 4' THEN 6 WHEN 'Офер' THEN 7 ELSE -1 END DESC,
  updated_at DESC,id DESC`, user)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var n, status string
		if err = rows.Scan(&n, &status); err != nil {
			break
		}
		if _, ok := s.Applications[n]; !ok {
			s.Applications[n] = applicationState{Status: status}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT company_name FROM company_vacancy_state")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			break
		}
		s.Initialized[n] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return s, err
	}
	return s, nil
}

func (r Repository) EntityDetail(ctx context.Context, user string, kind EntityType, id int64, name string) (map[string]any, error) {
	if r.DB == nil {
		return nil, errors.New("catalog database not configured")
	}
	if kind != CompanyType && kind != AgencyType {
		return nil, errors.New("unsupported entity type")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var data []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil {
		return nil, err
	}
	c, err := parseCatalog(data)
	if err != nil {
		return nil, err
	}
	var idValue any
	if id > 0 {
		var resolved string
		sqlText := "SELECT company_name FROM company_identity WHERE id=?"
		if kind == AgencyType {
			sqlText = "SELECT agency_name FROM agency_identity WHERE id=?"
		}
		err = tx.QueryRowContext(ctx, sqlText, id).Scan(&resolved)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			name = resolved
			idValue = id
		}
	}
	var e storedEntity
	for _, candidate := range c.entities(kind) {
		if nameOf(candidate) == name {
			e = candidate
			break
		}
	}
	if e == nil {
		return nil, ErrNotFound
	}
	var favorite, hidden int64
	err = tx.QueryRowContext(ctx, "SELECT favorite,hidden FROM company_user_state WHERE user_name=? AND company_name=? LIMIT 1", user, name).Scan(&favorite, &hidden)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var count int64
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM company_vacancies WHERE company_name=? OR real_company_name=?", name, name).Scan(&count); err != nil {
		return nil, err
	}
	result := projectDetail(e, kind, idValue, favorite != 0, hidden != 0, count)
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
