package crm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var ErrCompanyNotFound = errors.New("company not found")

type StatusChange struct {
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	ChangedAt  string `json:"changedAt"`
}
type CompanyState struct {
	Status    string         `json:"status"`
	Comment   string         `json:"comment"`
	Favorite  bool           `json:"favorite"`
	Hidden    bool           `json:"hidden"`
	UpdatedAt *string        `json:"updatedAt"`
	History   []StatusChange `json:"history"`
}

func isKnownCompany(data []byte, name string) (bool, error) {
	var catalog struct {
		Companies, Agencies []struct {
			Name string `json:"name"`
		}
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return false, err
	}
	if catalog.Companies == nil || catalog.Agencies == nil {
		return false, errors.New("incomplete catalog")
	}
	for _, group := range [][]struct {
		Name string `json:"name"`
	}{catalog.Companies, catalog.Agencies} {
		for _, record := range group {
			if record.Name == name {
				return true, nil
			}
		}
	}
	return false, nil
}

// Company reads the exact v316 /api/crm?company projection. No GET path
// modifies the legacy user_state table or initializes company_crm rows.
func (r Repository) Company(ctx context.Context, user, company string) (CompanyState, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || strings.TrimSpace(company) == "" {
		return CompanyState{}, errors.New("CRM DB, user or company unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return CompanyState{}, err
	}
	defer tx.Rollback()
	var data []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil {
		return CompanyState{}, err
	}
	known, err := isKnownCompany(data, company)
	if err != nil {
		return CompanyState{}, err
	}
	if !known {
		return CompanyState{}, ErrCompanyNotFound
	}
	result := CompanyState{History: []StatusChange{}}
	var favorite, hidden int64
	var updated sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT status,comment_md,favorite,hidden,updated_at
 FROM company_user_state WHERE user_name=? AND company_name=?
 ORDER BY updated_at DESC,favorite DESC,hidden DESC LIMIT 1`, user, company).
		Scan(&result.Status, &result.Comment, &favorite, &hidden, &updated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CompanyState{}, err
	}
	if updated.Valid && updated.String != "" {
		v := updated.String
		result.UpdatedAt = &v
	}
	result.Favorite = favorite != 0
	result.Hidden = hidden != 0
	rows, err := tx.QueryContext(ctx, `SELECT from_status,to_status,changed_at FROM company_status_history
 WHERE user_name=? AND company_name=? ORDER BY id DESC LIMIT 30`, user, company)
	if err != nil {
		return CompanyState{}, err
	}
	for rows.Next() {
		var change StatusChange
		if err = rows.Scan(&change.FromStatus, &change.ToStatus, &change.ChangedAt); err != nil {
			break
		}
		result.History = append(result.History, change)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return CompanyState{}, err
	}
	if err = tx.Commit(); err != nil {
		return CompanyState{}, err
	}
	return result, nil
}
