package parser

import (
	"context"
	"database/sql"
	"fmt"
)

// Внутри существующей owned транзакции кандидат может быть освобождён после
// смены поколения; LLM slot не перехватывается до окончания срока.
func acquireDetailLeases(ctx context.Context, tx *sql.Tx, j Job, candidateID int64, needSlot bool) (string, string, int64, error) {
	token, e := stageToken()
	if e != nil {
		return "", "", 0, e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return "", "", 0, ErrSettingsStorage
	}
	acquire := func(key string, candidate bool) (bool, error) {
		condition := "expires_at<=?"
		if candidate {
			condition = "generation<>VALUES(generation) OR expires_at<=?"
		}
		q := `INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE owner_token=IF(` + condition + `,VALUES(owner_token),owner_token),expires_at=IF(` + condition + `,VALUES(expires_at),expires_at),generation=IF(owner_token=VALUES(owner_token),VALUES(generation),generation)`
		if _, e := tx.ExecContext(ctx, q, key, j.Generation, token, now+300000, now, now); e != nil {
			return false, ErrSettingsStorage
		}
		var found string
		if e := tx.QueryRowContext(ctx, "SELECT owner_token FROM vacancy_detail_leases WHERE lease_key=?", key).Scan(&found); e != nil {
			return false, ErrSettingsStorage
		}
		return found == token, nil
	}
	ok, e := acquire(fmt.Sprintf("candidate:%d", candidateID), true)
	if e != nil {
		return "", "", 0, e
	}
	if !ok {
		return "", "", 0, ErrDetailBusy
	}
	if !needSlot {
		return token, "", now, nil
	}
	for _, key := range []string{"llm:1", "llm:2"} {
		ok, e = acquire(key, false)
		if e != nil {
			return "", "", 0, e
		}
		if ok {
			return token, key, now, nil
		}
	}
	return "", "", 0, ErrDetailBusy
}
func validateDetailLeases(ctx context.Context, tx *sql.Tx, j Job, candidateID int64, slot, token string, preflight bool) error {
	rows, e := tx.QueryContext(ctx, `SELECT lease_key FROM vacancy_detail_leases WHERE lease_key IN (?,?) AND generation=? AND owner_token=? AND expires_at>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) FOR UPDATE`, fmt.Sprintf("candidate:%d", candidateID), slot, j.Generation, token)
	if e != nil {
		return ErrSettingsStorage
	}
	n := 0
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			break
		}
		n++
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return ErrSettingsStorage
	}
	expected := 2
	if slot == "" && preflight {
		expected = 1
	}
	if n != expected {
		return ErrDetailBusy
	}
	return nil
}
func releaseOwnedDetail(ctx context.Context, tx *sql.Tx, j Job, candidateID int64, slot, token string, retain bool) error {
	if retain {
		return nil
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM vacancy_detail_leases WHERE lease_key IN (?,?) AND generation=? AND owner_token=?", fmt.Sprintf("candidate:%d", candidateID), slot, j.Generation, token); e != nil {
		return ErrSettingsStorage
	}
	return nil
}
