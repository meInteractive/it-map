package apifysettings

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"itmap/internal/legacycrypto"
)

var ErrStorage = errors.New("хранилище настроек Apify недоступно")
var ErrGeneration = errors.New("данные восстановлены во время изменения настроек Apify")
var ErrChanged = errors.New("настройки Apify изменились")
var ErrDecrypt = errors.New("ключ Apify не удалось расшифровать; сохраните его заново")
var ErrMissing = errors.New("ключ Apify не настроен")

type Repository struct {
	DB     *sql.DB
	secret string
}

func NewRepository(db *sql.DB, secret string) Repository { return Repository{DB: db, secret: secret} }
func (r Repository) String() string                      { return "apifysettings.Repository{credentials:redacted}" }
func (r Repository) GoString() string                    { return r.String() }

type sqlReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readStored(ctx context.Context, q sqlReader, lock bool) (StoredSettings, error) {
	var s StoredSettings
	var enabled, paid int64
	query := "SELECT enabled,paid_allowed,revision,free_key_encrypted,free_key_suffix,starter_key_encrypted,starter_key_suffix,free_account_json,starter_account_json,free_error,starter_error FROM apify_settings WHERE id=1"
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query).Scan(&enabled, &paid, &s.Revision, &s.FreeEncrypted, &s.FreeSuffix, &s.StarterEncrypted, &s.StarterSuffix, &s.FreeAccountJSON, &s.StarterAccountJSON, &s.FreeError, &s.StarterError)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil || (enabled != 0 && enabled != 1) || (paid != 0 && paid != 1) {
		return StoredSettings{}, ErrStorage
	}
	s.Enabled, s.PaidAllowed = enabled == 1, paid == 1
	return s, nil
}

func (r Repository) Read(ctx context.Context) (PublicView, error) {
	if r.DB == nil {
		return PublicView{}, ErrStorage
	}
	s, err := readStored(ctx, r.DB, false)
	if err != nil {
		return PublicView{}, err
	}
	return PublicSettings(s), nil
}

func (r Repository) Generation(ctx context.Context) (string, error) {
	if r.DB == nil {
		return "", ErrStorage
	}
	var g string
	if r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&g) != nil || utf8.RuneCountInString(g) != 36 {
		return "", ErrStorage
	}
	return g, nil
}

func (r Repository) lock(ctx context.Context, generation string) (*sql.Tx, StoredSettings, error) {
	if r.DB == nil {
		return nil, StoredSettings{}, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, StoredSettings{}, ErrStorage
	}
	var actual string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual) != nil {
		tx.Rollback()
		return nil, StoredSettings{}, ErrStorage
	}
	if actual != generation || utf8.RuneCountInString(actual) != 36 {
		tx.Rollback()
		return nil, StoredSettings{}, ErrGeneration
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO apify_settings(id) VALUES(1) ON DUPLICATE KEY UPDATE id=id"); err != nil {
		tx.Rollback()
		return nil, StoredSettings{}, ErrStorage
	}
	s, err := readStored(ctx, tx, true)
	if err != nil {
		tx.Rollback()
		return nil, StoredSettings{}, err
	}
	return tx, s, nil
}

func (r Repository) SaveKey(ctx context.Context, generation string, in KeyInput) (PublicView, error) {
	if (in.Profile != "free" && in.Profile != "starter") || !keyPattern.MatchString(in.APIKey) {
		return PublicView{}, ErrInput
	}
	codec, err := legacycrypto.New(r.secret, legacycrypto.Apify)
	if err != nil {
		return PublicView{}, ErrStorage
	}
	encrypted, err := codec.Encrypt(in.APIKey)
	if err != nil {
		return PublicView{}, ErrStorage
	}
	tx, s, err := r.lock(ctx, generation)
	if err != nil {
		return PublicView{}, err
	}
	defer tx.Rollback()
	if s.Revision != in.Revision {
		return PublicView{}, ErrChanged
	}
	query := "UPDATE apify_settings SET free_key_encrypted=?,free_key_suffix=?,free_account_json='',free_error='',paid_allowed=0,revision=revision+1 WHERE id=1 AND revision=?"
	if in.Profile == "starter" {
		query = "UPDATE apify_settings SET starter_key_encrypted=?,starter_key_suffix=?,starter_account_json='',starter_error='',paid_allowed=0,revision=revision+1 WHERE id=1 AND revision=?"
	}
	result, err := tx.ExecContext(ctx, query, encrypted, in.APIKey[len(in.APIKey)-4:], in.Revision)
	if err != nil {
		return PublicView{}, ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return PublicView{}, ErrChanged
	}
	s, err = readStored(ctx, tx, false)
	if err != nil {
		return PublicView{}, err
	}
	if tx.Commit() != nil {
		return PublicView{}, ErrStorage
	}
	return PublicSettings(s), nil
}

func (r Repository) SaveSettings(ctx context.Context, generation string, in SettingsInput) (PublicView, error) {
	tx, s, err := r.lock(ctx, generation)
	if err != nil {
		return PublicView{}, err
	}
	defer tx.Rollback()
	if s.Revision != in.Revision {
		return PublicView{}, ErrChanged
	}
	if err = ValidateTransition(in, s.FreeEncrypted != "", s.StarterEncrypted != "", s.PaidAllowed); err != nil {
		return PublicView{}, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE apify_settings SET enabled=?,paid_allowed=?,revision=revision+1 WHERE id=1 AND revision=?", in.Enabled, in.PaidAllowed, in.Revision)
	if err != nil {
		return PublicView{}, ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return PublicView{}, ErrChanged
	}
	s, err = readStored(ctx, tx, false)
	if err != nil {
		return PublicView{}, err
	}
	if tx.Commit() != nil {
		return PublicView{}, ErrStorage
	}
	return PublicSettings(s), nil
}

func (r Repository) Resolve(ctx context.Context, profile string) (string, error) {
	if r.DB == nil || (profile != "free" && profile != "starter") {
		return "", ErrInput
	}
	s, err := readStored(ctx, r.DB, false)
	if err != nil {
		return "", err
	}
	encrypted := s.FreeEncrypted
	if profile == "starter" {
		encrypted = s.StarterEncrypted
	}
	if encrypted == "" {
		return "", ErrMissing
	}
	codec, err := legacycrypto.New(r.secret, legacycrypto.Apify)
	if err != nil {
		return "", ErrDecrypt
	}
	key, err := codec.Decrypt(encrypted)
	if err != nil || !keyPattern.MatchString(key) {
		return "", ErrDecrypt
	}
	return key, nil
}
