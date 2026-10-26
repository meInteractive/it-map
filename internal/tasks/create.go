package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
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
	input, err := input.normalize()
	if err != nil {
		return 0, err
	}

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

// normalize preserves the typed legacy create contract before opening a transaction.
func (input CreateInput) normalize() (CreateInput, error) {
	input.CompanyName = trimTaskText(input.CompanyName)
	input.Note = trimTaskText(input.Note)
	if input.CompanyName == "" || utf8.RuneCountInString(input.CompanyName) > 255 || len(utf16.Encode([]rune(input.Note))) > 1200 {
		return input, ErrInvalidTask
	}
	switch input.Type {
	case "call", "email", "messenger", "meeting":
	default:
		return input, ErrInvalidTask
	}
	if input.VacancyID != nil && (*input.VacancyID <= 0 || *input.VacancyID > 1<<53-1) {
		return input, ErrInvalidTask
	}
	due, err := time.Parse(time.RFC3339Nano, input.DueAt)
	if err != nil {
		return input, ErrInvalidTask
	}
	input.DueAt = due.UTC().Format("2006-01-02T15:04:05.000Z")

	return input, nil
}

// ECMAScript trim excludes NEL and includes BOM, unlike strings.TrimSpace.
func trimTaskText(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		if r >= '\u2000' && r <= '\u200a' {
			return true
		}
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
			return true
		}
		return false
	})
}
