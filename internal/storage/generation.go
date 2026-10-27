package storage

import (
	"context"
	"database/sql"
	"errors"
)

var ErrGenerationChanged = errors.New("database restored during operation")

// WithinGenerationTx is the SQL boundary for delayed results (for example a
// synthetic/provider result obtained before restore). Capture expected BEFORE
// external work; call this only AFTER that work completes. No network operation
// belongs in fn. The generation lock and every callback statement share one TX,
// matching legacy generationBoundDb.batch without rewriting arbitrary SQL.
// The callback still owns its authorization, revision, lease and input guards.
func WithinGenerationTx(ctx context.Context, db *sql.DB, expected string, fn func(*sql.Tx) error) error {
	if expected == "" || fn == nil {
		return errors.New("generation transaction requires context and callback")
	}
	return WithinTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		var current string
		if e := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&current); e != nil {
			return e
		}
		if current != expected {
			return ErrGenerationChanged
		}
		return fn(tx)
	})
}
