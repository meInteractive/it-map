// Package tasks ports read-only user-scoped CRM task listing from Sites v316.
package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrCompanyNotFound = errors.New("company not found")

type Task struct {
	ID                  int64   `json:"id"`
	CompanyName         string  `json:"companyName"`
	VacancyID           *int64  `json:"vacancyId"`
	VacancyTitle        string  `json:"vacancyTitle"`
	Type                string  `json:"type"`
	Note                string  `json:"note"`
	DueAt               string  `json:"dueAt"`
	Completed           bool    `json:"completed"`
	GoogleEventID       string  `json:"googleEventId"`
	GoogleEventHTMLLink string  `json:"googleEventHtmlLink"`
	GoogleSyncStatus    string  `json:"googleSyncStatus"`
	GoogleSyncError     string  `json:"googleSyncError"`
	CreatedAt           string  `json:"createdAt"`
	CompletedAt         *string `json:"completedAt"`
}

type Entity struct {
	Name       string `json:"name"`
	Rank       any    `json:"rank"`
	EntityType string `json:"entityType"`
}

type Response struct {
	Tasks    []Task   `json:"tasks"`
	Entities []Entity `json:"entities"`
}

type Repository struct{ DB *sql.DB }

func entitiesForTasks(data []byte, names map[string]bool, company string) ([]Entity, error) {
	var source struct{ Companies, Agencies []map[string]any }
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("invalid catalog JSON: %w", err)
	}
	if source.Companies == nil || source.Agencies == nil {
		return nil, errors.New("catalog missing organizations")
	}
	foundCompany := company == ""
	result := []Entity{}
	for _, group := range []struct {
		entries []map[string]any
		kind    string
	}{{source.Companies, "company"}, {source.Agencies, "agency"}} {
		for _, item := range group.entries {
			name, _ := item["name"].(string)
			if company == name {
				foundCompany = true
			}
			if !names[name] {
				continue
			}
			result = append(result, Entity{Name: name, Rank: item["rank"], EntityType: group.kind})
		}
	}
	if !foundCompany {
		return nil, ErrCompanyNotFound
	}
	return result, nil
}

// List uses a consistent snapshot, has no write side effects, and scopes rows
// by the signed-in user's canonical name. It never falls back to the admin.
func (r Repository) List(ctx context.Context, user, company string, includeCompleted bool, vacancyID *int64) (Response, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return Response{}, errors.New("task DB/user unavailable")
	}
	if vacancyID != nil && *vacancyID <= 0 {
		return Response{}, errors.New("invalid vacancy ID")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Response{}, err
	}
	defer tx.Rollback()
	var catalogJSON []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalogJSON); err != nil {
		return Response{}, err
	}
	// Validate the selected company before reading any private task records.
	if company != "" {
		if _, err = entitiesForTasks(catalogJSON, nil, company); err != nil {
			return Response{}, err
		}
	}
	query := `SELECT task.id,task.company_name,task.vacancy_id,COALESCE(v.title,''),
        task.type,task.note,task.due_at,task.completed,
        COALESCE(task.google_event_id,''),COALESCE(task.google_event_html_link,''),
        task.google_sync_status,task.google_sync_error,task.created_at,task.completed_at
        FROM company_tasks task LEFT JOIN company_vacancies v ON v.id=task.vacancy_id
        WHERE task.user_name=?`
	args := []any{user}
	if company != "" {
		query += " AND task.company_name=?"
		args = append(args, company)
	}
	if vacancyID != nil {
		query += " AND task.vacancy_id=?"
		args = append(args, *vacancyID)
	}
	if !includeCompleted {
		query += " AND task.completed=0"
	}
	query += " ORDER BY task.completed ASC,task.due_at ASC,task.id ASC"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return Response{}, err
	}
	out := Response{Tasks: []Task{}, Entities: []Entity{}}
	names := map[string]bool{}
	for rows.Next() {
		var task Task
		var vacancy sql.NullInt64
		var completed int64
		var completedAt sql.NullString
		err = rows.Scan(&task.ID, &task.CompanyName, &vacancy, &task.VacancyTitle, &task.Type, &task.Note,
			&task.DueAt, &completed, &task.GoogleEventID, &task.GoogleEventHTMLLink,
			&task.GoogleSyncStatus, &task.GoogleSyncError, &task.CreatedAt, &completedAt)
		if err != nil {
			break
		}
		if vacancy.Valid {
			id := vacancy.Int64
			task.VacancyID = &id
		}
		if completedAt.Valid && completedAt.String != "" {
			text := completedAt.String
			task.CompletedAt = &text
		}
		task.Completed = completed != 0
		out.Tasks = append(out.Tasks, task)
		names[task.CompanyName] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return Response{}, err
	}
	out.Entities, err = entitiesForTasks(catalogJSON, names, "")
	if err != nil {
		return Response{}, err
	}
	if err = tx.Commit(); err != nil {
		return Response{}, err
	}
	return out, nil
}
