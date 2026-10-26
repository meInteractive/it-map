package crm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"itmap/internal/vacancies"
)

var ErrInvalidCompanyInput = errors.New("invalid company CRM status or comment")

type SaveInput struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

func (in SaveInput) Validate() error {
	known := false
	for _, status := range []string{"", "Откликнулся", "Получил ответ", "Скрининг", "Собес 2", "Собес 3", "Собес 4", "Офер", "Отказ"} {
		if in.Status == status {
			known = true
			break
		}
	}
	if !known || len(utf16.Encode([]rune(in.Comment))) > 1000 {
		return ErrInvalidCompanyInput
	}
	return nil
}

type SaveResult struct {
	CompanyState
	Vacancies []vacancies.CompanyVacancy `json:"vacancies"`
}

// SaveCompany preserves PUT /api/crm's whole-form last-writer semantics. The
// legacy company CRM API has no revision token. Locking before reading status
// serializes the history chain and keeps the independent personal flags intact.
func (r Repository) SaveCompany(ctx context.Context, user, company string, input SaveInput) (SaveResult, error) {
	if err := input.Validate(); err != nil {
		return SaveResult{}, err
	}
	if r.DB == nil || strings.TrimSpace(user) == "" || company == "" || utf8.RuneCountInString(company) > 255 {
		return SaveResult{}, ErrCompanyNotFound
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SaveResult{}, err
	}
	defer tx.Rollback()
	if err = lockCompany(ctx, tx, company); err != nil {
		return SaveResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT status FROM company_user_state WHERE user_name=? AND company_name=? FOR UPDATE`, user, company)
	if err != nil {
		return SaveResult{}, err
	}
	previous := ""
	count := 0
	for rows.Next() {
		if err = rows.Scan(&previous); err != nil {
			break
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return SaveResult{}, err
	}
	if count > 1 {
		return SaveResult{}, ErrAmbiguousState
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO company_user_state(user_name,company_name,status,comment_md,updated_at) VALUES(?,?,?,?,UTC_TIMESTAMP())`, user, company, input.Status, input.Comment)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE company_user_state SET status=?,comment_md=?,updated_at=UTC_TIMESTAMP() WHERE user_name=? AND company_name=?`, input.Status, input.Comment, user, company)
	}
	if err != nil {
		return SaveResult{}, err
	}
	if previous != input.Status {
		_, err = tx.ExecContext(ctx, `INSERT INTO company_status_history(user_name,company_name,from_status,to_status,changed_at) VALUES(?,?,?,?,UTC_TIMESTAMP())`, user, company, previous, input.Status)
		if err != nil {
			return SaveResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return SaveResult{}, err
	}
	state, err := r.Company(ctx, user, company)
	if err != nil {
		return SaveResult{}, err
	}
	list, err := (vacancies.Repository{DB: r.DB}).CompanyVacancies(ctx, user, company)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{CompanyState: state, Vacancies: list}, nil
}

// The unique global parent provides an exclusive serialization lock without
// writing shared status/comment/favorite. Callers must retain it until commit.
// Keep the no-op UPSERT: INSERT IGNORE can take shared duplicate-key locks
// whose concurrent upgrades to FOR UPDATE deadlock.
func lockCompany(ctx context.Context, tx *sql.Tx, company string) error {
	var catalog []byte
	if err := tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&catalog); err != nil {
		return err
	}
	known, err := isKnownCompany(catalog, company)
	if err != nil {
		return err
	}
	if !known {
		return ErrCompanyNotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)`, company); err != nil {
		return err
	}
	var locked string
	return tx.QueryRowContext(ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, company).Scan(&locked)
}
