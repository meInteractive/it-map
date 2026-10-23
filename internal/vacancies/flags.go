package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// UserFlags is the personal-only v316 PUT /api/vacancies/:id/state response.
// No company-wide vacancy fields are changed by these operations.
type UserFlags struct {
	VacancyID int64 `json:"vacancyId"`
	Favorite  bool  `json:"favorite"`
	Hidden    bool  `json:"hidden"`
}

var ErrInvalidFlag = errors.New("unsupported vacancy flag")

// SetUserFlag holds a row lock on the canonical vacancy before looking up or
// creating its user-scoped state. This serializes first-time writes even while
// the original D1 -> MariaDB dump lacks the (user_name,vacancy_id) unique key.
// The update changes ALL pre-existing duplicate state rows to the same flags
// while preserving each row's relevance. No schema rewrite is required in 1.1.
func (r Repository) SetUserFlag(ctx context.Context, user string, id int64, key string, value bool) (UserFlags, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id < 1 {
		return UserFlags{}, errors.New("user flag storage unavailable")
	}
	if key != "favorite" && key != "hidden" {
		return UserFlags{}, ErrInvalidFlag
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return UserFlags{}, err
	}
	defer tx.Rollback()
	var exists int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM company_vacancies WHERE id=? FOR UPDATE", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return UserFlags{}, ErrNotFound
	}
	if err != nil {
		return UserFlags{}, err
	}
	var fav, hidden int64
	err = tx.QueryRowContext(ctx, `SELECT favorite,hidden FROM company_vacancy_user_state
        WHERE user_name=? AND vacancy_id=?
        ORDER BY updated_at DESC,favorite DESC,hidden DESC,relevance DESC LIMIT 1`, user, id).Scan(&fav, &hidden)
	present := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return UserFlags{}, err
	}
	if key == "favorite" {
		fav = flagInt(value)
	} else {
		hidden = flagInt(value)
	}
	if present {
		// If old DB contains duplicates, ensure every row has the same flags.
		_, err = tx.ExecContext(ctx, `UPDATE company_vacancy_user_state SET favorite=?,hidden=?,updated_at=CURRENT_TIMESTAMP
            WHERE user_name=? AND vacancy_id=?`, fav, hidden, user, id)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state
            (user_name,vacancy_id,favorite,hidden,updated_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`, user, id, fav, hidden)
	}
	if err != nil {
		return UserFlags{}, err
	}
	if err = tx.Commit(); err != nil {
		return UserFlags{}, err
	}
	return UserFlags{VacancyID: id, Favorite: fav != 0, Hidden: hidden != 0}, nil
}
func flagInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
