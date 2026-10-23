package crm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrAmbiguousState = errors.New("duplicate CRM user state requires reconciliation")
var ErrInvalidFlag = errors.New("unsupported company flag")

// SetCompanyFlag changes only the signed-in user's favorite/hidden company flag.
// company_crm provides a stable, unique per-company serialization lock even
// though company_user_state lost its unique(user_name,company_name) constraint
// during D1 -> MariaDB conversion. Existing duplicate state is NOT silently
// merged or overwritten: resolve duplicates on staging before enabling them.
func (r Repository) SetCompanyFlag(ctx context.Context, user, company, flag string, value bool) (CompanyState, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || company == "" || utf8.RuneCountInString(company) > 255 {
		return CompanyState{}, ErrCompanyNotFound
	}
	if flag != "favorite" && flag != "hidden" {
		return CompanyState{}, ErrInvalidFlag
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CompanyState{}, err
	}
	defer tx.Rollback()
	var catalog []byte
	if err = tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&catalog); err != nil {
		return CompanyState{}, err
	}
	known, err := isKnownCompany(catalog, company)
	if err != nil {
		return CompanyState{}, err
	}
	if !known {
		return CompanyState{}, ErrCompanyNotFound
	}
	// Stable existing lock row, created once, is safe under the company_crm PK.
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO company_crm(company_name) VALUES(?)`, company); err != nil {
		return CompanyState{}, err
	}
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, company).Scan(&locked); err != nil {
		return CompanyState{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT favorite,hidden FROM company_user_state
        WHERE user_name=? AND company_name=? FOR UPDATE`, user, company)
	if err != nil {
		return CompanyState{}, err
	}
	var favorite, hidden int64
	count := 0
	for rows.Next() {
		if err = rows.Scan(&favorite, &hidden); err != nil {
			break
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return CompanyState{}, err
	}
	if count > 1 {
		return CompanyState{}, ErrAmbiguousState
	}
	if flag == "favorite" {
		favorite = boolInt(value)
	} else {
		hidden = boolInt(value)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO company_user_state
            (user_name,company_name,favorite,hidden,updated_at)
            VALUES(?,?,?,?,UTC_TIMESTAMP())`, user, company, favorite, hidden)
	} else {
		// Do not touch the user's independent comment or CRM status.
		_, err = tx.ExecContext(ctx, `UPDATE company_user_state SET favorite=?,hidden=?,updated_at=UTC_TIMESTAMP()
            WHERE user_name=? AND company_name=?`, favorite, hidden, user, company)
	}
	if err != nil {
		return CompanyState{}, err
	}
	if err = tx.Commit(); err != nil {
		return CompanyState{}, err
	}
	return r.Company(ctx, user, company)
}
func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
