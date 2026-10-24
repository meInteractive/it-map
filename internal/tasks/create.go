package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrVacancyNotInCompany = errors.New("vacancy does not belong to company")
var ErrInvalidTask = errors.New("invalid task payload")

type CreateInput struct {
	CompanyName string `json:"companyName"`
	VacancyID   *int64 `json:"vacancyId"`
	Type        string `json:"type"`
	Note        string `json:"note"`
	DueAt       string `json:"dueAt"`
}

func (r Repository) Create(ctx context.Context, user string, input CreateInput) (int64, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return 0, errors.New("task DB or user unavailable")
	}
	input.CompanyName = strings.TrimSpace(input.CompanyName)
	input.Note = strings.TrimSpace(input.Note)
	if input.CompanyName == "" || utf8.RuneCountInString(input.CompanyName) > 255 || utf8.RuneCountInString(input.Note) > 1200 {
		return 0, ErrInvalidTask
	}
	switch input.Type {
	case "call", "email", "messenger", "meeting":
	default:
		return 0, ErrInvalidTask
	}
	if input.VacancyID != nil && *input.VacancyID <= 0 {
		return 0, ErrInvalidTask
	}
	due, err := time.Parse(time.RFC3339Nano, input.DueAt)
	if err != nil {
		return 0, ErrInvalidTask
	}
	input.DueAt = due.UTC().Format("2006-01-02T15:04:05.000Z")

	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var catalog []byte
	if err = tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&catalog); err != nil {
		return 0, err
	}
	if _, err = entitiesForTasks(catalog, nil, input.CompanyName); err != nil {
		return 0, err
	}

	// This port must not silently skip Google Calendar synchronization.
	var connected int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM google_calendar_user_connection WHERE user_name=? LIMIT 1 FOR UPDATE`, user).Scan(&connected)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if connected == 1 {
		return 0, ErrCalendarSyncRequired
	}

	if input.VacancyID != nil {
		var matches int64
		err = tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM company_vacancies WHERE id=? AND (company_name=? OR real_company_name=?)`,
			*input.VacancyID, input.CompanyName, input.CompanyName).Scan(&matches)
		if err != nil {
			return 0, err
		}
		if matches != 1 {
			return 0, ErrVacancyNotInCompany
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO company_crm (company_name) VALUES (?) ON DUPLICATE KEY UPDATE company_name=company_name`, input.CompanyName)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO company_tasks (user_name,company_name,vacancy_id,type,note,due_at) VALUES (?,?,?,?,?,?)`,
		user, input.CompanyName, input.VacancyID, input.Type, input.Note, input.DueAt)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, errors.New("task insert did not return an ID")
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
