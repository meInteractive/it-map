// Package applications ports the user-scoped, read-only CRM application
// projections. Mutating an application requires a separate revision-safe port.
package applications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var ErrNotFound = errors.New("application not found")

type StatusChange struct {
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	ChangedAt  string `json:"changedAt"`
}

type Record struct {
	ID                   int64          `json:"id"`
	Revision             string         `json:"revision"`
	CompanyName          string         `json:"companyName"`
	VacancyID            *int64         `json:"vacancyId"`
	VacancyTitle         string         `json:"vacancyTitle"`
	Status               string         `json:"status"`
	Comment              string         `json:"comment"`
	RecruiterName        string         `json:"recruiterName"`
	RecruiterPhone       string         `json:"recruiterPhone"`
	RecruiterEmail       string         `json:"recruiterEmail"`
	RecruiterContactType string         `json:"recruiterContactType"`
	ApplicationSources   []string       `json:"applicationSources"`
	CreatedAt            string         `json:"createdAt"`
	UpdatedAt            string         `json:"updatedAt"`
	History              []StatusChange `json:"history"`
}

type Entity struct {
	Name       string `json:"name"`
	Rank       any    `json:"rank"`
	EntityType string `json:"entityType"`
}

type CRMResponse struct {
	Records  []Record `json:"records"`
	Entities []Entity `json:"entities"`
}

type Repository struct {
	DB             *sql.DB
	CreationSecret []byte
}

func AllowedSources(raw string) []string {
	var input []string
	_ = json.Unmarshal([]byte(raw), &input)
	allowed := map[string]bool{"hh": true, "site": true, "habr": true, "email": true, "telegram": true, "hcm": true}
	result := []string{}
	seen := map[string]bool{}
	for _, s := range input {
		s = strings.TrimSpace(s)
		if allowed[s] && !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	if len(result) == 0 {
		return []string{"hh"}
	}
	return result
}

// Fingerprint is the same SHA-256 operation as the Sites application revision
// function. It deliberately hashes the database's JSON_ARRAY state snapshot,
// not just the timestamp, to detect concurrent or same-second changes.
func Fingerprint(snapshot string) string {
	sum := sha256.Sum256([]byte(snapshot))
	return hex.EncodeToString(sum[:])
}

// recordSnapshotColumns preserves the exact legacy column order. This
// expression must also be used by eventual Go PUT optimistic-locking.
const recordSnapshotColumns = `JSON_ARRAY(a.id,a.user_name,a.company_name,a.vacancy_id,a.status,a.comment_md,
 a.recruiter_name,a.recruiter_phone,a.recruiter_email,a.recruiter_contact_type,
 a.application_sources,a.created_at,a.updated_at)`

func (r Repository) List(ctx context.Context, user, company string, id *int64) ([]Record, error) {
	records, _, err := r.read(ctx, user, company, id, false)
	return records, err
}

func (r Repository) ByID(ctx context.Context, user string, id int64) (Record, error) {
	if id < 1 {
		return Record{}, ErrNotFound
	}
	records, err := r.List(ctx, user, "", &id)
	if err != nil {
		return Record{}, err
	}
	if len(records) == 0 {
		return Record{}, ErrNotFound
	}
	return records[0], nil
}

func (r Repository) CRM(ctx context.Context, user string) (CRMResponse, error) {
	records, catalog, err := r.read(ctx, user, "", nil, true)
	if err != nil {
		return CRMResponse{}, err
	}
	names := make(map[string]bool, len(records))
	for _, x := range records {
		names[x.CompanyName] = true
	}
	entities, err := entityDirectory(catalog, names)
	if err != nil {
		return CRMResponse{}, err
	}
	return CRMResponse{Records: records, Entities: entities}, nil
}

func entityDirectory(data []byte, names map[string]bool) ([]Entity, error) {
	var catalog struct{ Companies, Agencies []map[string]any }
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	if catalog.Companies == nil || catalog.Agencies == nil {
		return nil, errors.New("incomplete entity catalog")
	}
	entities := []Entity{}
	for _, set := range []struct {
		rows []map[string]any
		kind string
	}{{catalog.Companies, "company"}, {catalog.Agencies, "agency"}} {
		for _, row := range set.rows {
			name, _ := row["name"].(string)
			if !names[name] {
				continue
			}
			entities = append(entities, Entity{Name: name, Rank: row["rank"], EntityType: set.kind})
		}
	}
	return entities, nil
}

// read uses one repeatable-read snapshot for application rows, status history,
// and optional entity index. The query is always scoped by user_name.
func (r Repository) read(ctx context.Context, user, company string, id *int64, withCatalog bool) ([]Record, []byte, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return nil, nil, errors.New("application DB/user unavailable")
	}
	if id != nil && *id < 1 {
		return nil, nil, errors.New("invalid application ID")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	where := "a.user_name=?"
	args := []any{user}
	if company != "" {
		where += " AND a.company_name=?"
		args = append(args, company)
	}
	if id != nil {
		where += " AND a.id=?"
		args = append(args, *id)
	}
	const fields = `a.id,a.company_name,a.vacancy_id,COALESCE(v.title,''),
 COALESCE(a.status,''),COALESCE(a.comment_md,''),COALESCE(a.recruiter_name,''),
 COALESCE(a.recruiter_phone,''),COALESCE(a.recruiter_email,''),COALESCE(a.recruiter_contact_type,''),
 COALESCE(a.application_sources,''),a.created_at,a.updated_at,`
	query := `SELECT ` + fields + recordSnapshotColumns + ` FROM company_applications a
 LEFT JOIN company_vacancies v ON v.id=a.vacancy_id
 WHERE ` + where + ` ORDER BY a.updated_at DESC,a.id DESC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	result := []Record{}
	indexes := make(map[int64]int)
	for rows.Next() {
		var rec Record
		var vacancy sql.NullInt64
		var sources, snapshot string
		err = rows.Scan(&rec.ID, &rec.CompanyName, &vacancy, &rec.VacancyTitle, &rec.Status, &rec.Comment,
			&rec.RecruiterName, &rec.RecruiterPhone, &rec.RecruiterEmail, &rec.RecruiterContactType,
			&sources, &rec.CreatedAt, &rec.UpdatedAt, &snapshot)
		if err != nil {
			break
		}
		if vacancy.Valid {
			n := vacancy.Int64
			rec.VacancyID = &n
		}
		rec.ApplicationSources = AllowedSources(sources)
		rec.Revision = Fingerprint(snapshot)
		rec.History = []StatusChange{}
		indexes[rec.ID] = len(result)
		result = append(result, rec)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	historyQuery := `SELECT h.application_id,COALESCE(h.from_status,''),COALESCE(h.to_status,''),h.changed_at
 FROM company_application_status_history h
 JOIN company_applications a ON a.id=h.application_id WHERE ` + where + ` ORDER BY h.id DESC`
	rows, err = tx.QueryContext(ctx, historyQuery, args...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var appID int64
		var change StatusChange
		err = rows.Scan(&appID, &change.FromStatus, &change.ToStatus, &change.ChangedAt)
		if err != nil {
			break
		}
		if i, ok := indexes[appID]; ok {
			result[i].History = append(result[i].History, change)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	var catalog []byte
	if withCatalog {
		if err = tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&catalog); err != nil {
			return nil, nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return result, catalog, nil
}
