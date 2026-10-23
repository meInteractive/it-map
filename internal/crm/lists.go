// Package crm contains authenticated user-level company CRM projections.
package crm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Repository struct{ DB *sql.DB }

// CompanyNames reads per-user company flags, never the global company_crm
// defaults. DISTINCT compensates for the migrated D1 schema's missing UNIQUE.
func (r Repository) CompanyNames(ctx context.Context, user string, flag string) ([]string, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return nil, errors.New("CRM database/user unavailable")
	}
	if flag != "favorite" && flag != "hidden" {
		return nil, errors.New("unsupported CRM flag")
	}
	// Column name must come from the above allowlist, never from request input.
	query := `SELECT DISTINCT company_name FROM company_user_state WHERE user_name=? AND ` + flag + `=1 ORDER BY company_name ASC`
	rows, err := r.DB.QueryContext(ctx, query, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}
