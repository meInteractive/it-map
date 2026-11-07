package apifyruns

import (
	"context"
	"database/sql"
	"regexp"
	"time"
)

var safeDiagnosticCode = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type OperationDebugItem struct {
	OperationID string   `json:"operationId"`
	Profile     string   `json:"profile"`
	State       string   `json:"state"`
	CreatedAt   int64    `json:"createdAt"`
	UpdatedAt   int64    `json:"updatedAt"`
	ChargedUSD  *float64 `json:"chargedUsd"`
	ReservedUSD float64  `json:"reservedUsd"`
	ErrorCode   *string  `json:"errorCode"`
}

type OperationsDebugView struct {
	Items     []OperationDebugItem `json:"items"`
	Total     int64                `json:"total"`
	Limit     int                  `json:"limit"`
	Coverage  string               `json:"coverage"`
	CheckedAt int64                `json:"checkedAt"`
}

// ReadOperationsDebug читает десять последних операций, включая прошлые
// поколения. Это единицы оплачиваемого Actor, а не количество запросов GET
// Dataset или повторных попыток. Токены, аккаунты и ID Dataset не читаются.
func (r Repository) ReadOperationsDebug(ctx context.Context) (OperationsDebugView, error) {
	out := OperationsDebugView{
		Items: []OperationDebugItem{}, Limit: 10, Coverage: "server_latest",
	}
	if r.DB == nil {
		return out, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, ErrStorage
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&out.Total); err != nil {
		return out, ErrStorage
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,profile,state,created_at,updated_at,charged_usd,reserved_usd,error_code
		FROM apify_runs ORDER BY created_at DESC,BINARY id DESC LIMIT 10`)
	if err != nil {
		return out, ErrStorage
	}
	for rows.Next() {
		var item OperationDebugItem
		var charged sql.NullFloat64
		var errorCode string
		if err = rows.Scan(&item.OperationID, &item.Profile, &item.State,
			&item.CreatedAt, &item.UpdatedAt, &charged, &item.ReservedUSD, &errorCode); err != nil {
			break
		}
		if !safeRunID.MatchString(item.OperationID) ||
			!safeDiagnosticCode.MatchString(item.Profile) ||
			!safeDiagnosticCode.MatchString(item.State) {
			err = ErrStorage
			break
		}
		if charged.Valid {
			item.ChargedUSD = &charged.Float64
		}
		if safeDiagnosticCode.MatchString(errorCode) {
			item.ErrorCode = &errorCode
		}
		out.Items = append(out.Items, item)
	}
	if err == nil { err = rows.Err() }
	rows.Close()
	if err != nil { return OperationsDebugView{}, ErrStorage }
	out.CheckedAt = time.Now().UTC().UnixMilli()
	if err = tx.Commit(); err != nil { return OperationsDebugView{}, ErrStorage }
	return out, nil
}
