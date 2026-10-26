package profiles

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrConflict = errors.New("profile/settings revision conflict")
var ErrUnavailable = errors.New("profile/settings storage or signed user unavailable")

type Repository struct{ DB *sql.DB }

type profileRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readResumeRow(ctx context.Context, q profileRowQueryer, user string) (Resume, error) {
	result := DefaultResume()
	var updated sql.NullString
	err := q.QueryRowContext(ctx, `SELECT resume_md,revision,updated_at FROM user_profiles WHERE user_name=?`, user).Scan(&result.ResumeMD, &result.Revision, &updated)
	if err != nil {
		return Resume{}, err
	}
	if updated.Valid {
		value := updated.String
		result.UpdatedAt = &value
	}
	return result, nil
}

// ReadResume never initializes or migrates storage on GET. An absent row is the
// legacy empty profile; database failures are not reported as an empty resume.
func (r Repository) ReadResume(ctx context.Context, user string) (Resume, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return Resume{}, ErrUnavailable
	}
	result, err := readResumeRow(ctx, r.DB, user)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultResume(), nil
	}
	return result, err
}

// SaveResume serializes first inserts and edits under the same primary-key lock.
// Bootstrap, revision check and update all roll back on conflict/failure.
func (r Repository) SaveResume(ctx context.Context, user string, input TextInput) (Resume, error) {
	if err := ValidateResume(input); err != nil {
		return Resume{}, err
	}
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return Resume{}, ErrUnavailable
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resume{}, err
	}
	defer tx.Rollback()
	// No-op UPSERT acquires an exclusive lock immediately, avoiding duplicate-key
	// shared locks and concurrent FOR UPDATE upgrades on a first-time profile.
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_profiles(user_name) VALUES(?) ON DUPLICATE KEY UPDATE user_name=VALUES(user_name)`, user); err != nil {
		return Resume{}, err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM user_profiles WHERE user_name=? FOR UPDATE`, user).Scan(&revision); err != nil {
		return Resume{}, err
	}
	if revision != input.Revision {
		return Resume{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE user_profiles SET resume_md=?,revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE user_name=? AND revision=?`, input.Text, user, input.Revision)
	if err != nil {
		return Resume{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Resume{}, err
	}
	if count != 1 {
		return Resume{}, ErrConflict
	}
	saved, err := readResumeRow(ctx, tx, user)
	if err != nil {
		return Resume{}, err
	}
	if err = tx.Commit(); err != nil {
		return Resume{}, err
	}
	return saved, nil
}
