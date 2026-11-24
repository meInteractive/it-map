package profiles

import (
	"context"
	"database/sql"
	"errors"
	"itmap/internal/dbretry"
	"strings"
)

var ErrGeneration = errors.New("profile model generation changed")

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
	// Повторяем целую транзакцию только после подтверждённого MariaDB deadlock rollback.
	// Исходная revision повторно проверяется под блокировкой на каждой попытке.
	return dbretry.Run(ctx, func() (Resume, error) { return r.saveResumeOnce(ctx, user, input, nil) })
}

func (r Repository) saveResumeOnce(ctx context.Context, user string, input TextInput, portfolioParts []string) (Resume, error) {
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
	for i, part := range portfolioParts {
		key := user + portfolioSuffixes[i]
		if part == "" {
			_, err = tx.ExecContext(ctx, "DELETE FROM user_profiles WHERE user_name=?", key)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO user_profiles(user_name,resume_md,revision) VALUES(?,?,?)
				ON DUPLICATE KEY UPDATE resume_md=VALUES(resume_md),revision=VALUES(revision)`, key, part, revision+1)
		}
		if err != nil {
			return Resume{}, err
		}
	}
	saved, err := readResumeRow(ctx, tx, user)
	if err != nil {
		return Resume{}, err
	}
	if err = tx.Commit(); err != nil {
		return Resume{}, err
	}
	if portfolioParts != nil {
		saved.ResumeMD += strings.Join(portfolioParts, "")
		saved.MaxLength = MaxPortfolioLength
	}
	return saved, nil
}

func (r Repository) Generation(ctx context.Context) (string, error) {
	if r.DB == nil {
		return "", ErrUnavailable
	}
	var g string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&g); e != nil || len(g) != 36 {
		return "", ErrUnavailable
	}
	return g, nil
}

// PortfolioProfileKey резервирует отдельную запись Markdown в user_profiles.
// Имена с этим префиксом не выдаются авторизованным пользователям. Запись
// использует существующие ограничения, независимую версию и экспорт профилей.
func PortfolioProfileKey(user string) string {
	return "portfolio:" + user
}

// Для длинного портфолио дополнительные строки сохраняются в той же
// транзакции. Начальная строка сохраняет существующие данные и версию.
const portfolioPart2 = ":2"
const portfolioPart3 = ":3"

var portfolioSuffixes = [...]string{portfolioPart2, portfolioPart3}

// PortfolioTextSQL и PortfolioPartsSQL используют алиас portfolio для
// основной записи и собирают все части в одном SQL-снимке.
const PortfolioTextSQL = "CONCAT(COALESCE(portfolio.resume_md,''),COALESCE(portfolio2.resume_md,''),COALESCE(portfolio3.resume_md,''))"
const PortfolioPartsSQL = `
 LEFT JOIN user_profiles portfolio2 ON portfolio2.user_name=CONCAT(portfolio.user_name,'` + portfolioPart2 + `') AND portfolio2.revision=portfolio.revision
 LEFT JOIN user_profiles portfolio3 ON portfolio3.user_name=CONCAT(portfolio.user_name,'` + portfolioPart3 + `') AND portfolio3.revision=portfolio.revision`

func (r Repository) ReadPortfolio(ctx context.Context, user string) (Resume, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return Resume{}, ErrUnavailable
	}
	result := Resume{MaxLength: MaxPortfolioLength}
	var updated sql.NullString
	err := r.DB.QueryRowContext(ctx, "SELECT "+PortfolioTextSQL+",portfolio.revision,portfolio.updated_at FROM user_profiles portfolio"+PortfolioPartsSQL+" WHERE portfolio.user_name=?", PortfolioProfileKey(user)).Scan(&result.ResumeMD, &result.Revision, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return Resume{}, err
	}
	if updated.Valid {
		result.UpdatedAt = &updated.String
	}
	return result, nil
}

func (r Repository) SavePortfolio(ctx context.Context, user string, input TextInput) (Resume, error) {
	if err := validateProfileText(input, true); err != nil {
		return Resume{}, err
	}
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return Resume{}, ErrUnavailable
	}
	text := []rune(input.Text)
	var overflow [len(portfolioSuffixes)]string
	for i := range overflow {
		start := (i + 1) * MaxResumeLength
		if start < len(text) {
			overflow[i] = string(text[start:min(start+MaxResumeLength, len(text))])
		}
	}
	input.Text = string(text[:min(MaxResumeLength, len(text))])
	return dbretry.Run(ctx, func() (Resume, error) {
		return r.saveResumeOnce(ctx, PortfolioProfileKey(user), input, overflow[:])
	})
}
