package apifysettings

import (
	"context"
	"database/sql"
	"unicode/utf8"

	"itmap/internal/legacycrypto"
)

// Версии берутся из серверного задания, а не из входящего HTTP-запроса.
type OperationVersion struct {
	Generation       string
	SettingsRevision int64
	CompanyID        int64
	SourceURL        string
	SourceRevision   int64
	Profile          string
}

func validateOperation(v OperationVersion, generation string, settings StoredSettings, source HHSource) error {
	if v.Profile != "free" && v.Profile != "starter" {
		return ErrInput
	}
	if v.Generation != generation || utf8.RuneCountInString(generation) != 36 {
		return ErrGeneration
	}
	if v.SettingsRevision != settings.Revision || v.CompanyID != source.CompanyID || v.SourceURL != source.URL || v.SourceRevision != source.Revision {
		return ErrChanged
	}
	if !settings.Enabled || !source.Enabled || !source.Supported || v.Profile == "starter" && !settings.PaidAllowed {
		return ErrDisabled
	}
	return nil
}

// Проверка читает согласованный снимок. Фиксация результата внешней операции
// должна повторно проверять эти версии в транзакции денежного журнала GO-398.
func (r Repository) ResolveOperation(ctx context.Context, v OperationVersion) (string, error) {
	if r.DB == nil {
		return "", ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", ErrStorage
	}
	defer tx.Rollback()
	var generation string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation) != nil {
		return "", ErrStorage
	}
	s, err := readStored(ctx, tx, false)
	if err != nil {
		return "", err
	}
	source, _, err := readHHSource(ctx, tx, v.CompanyID, false)
	if err != nil {
		return "", err
	}
	if err = validateOperation(v, generation, s, source); err != nil {
		return "", err
	}
	encrypted := s.FreeEncrypted
	if v.Profile == "starter" {
		encrypted = s.StarterEncrypted
	}
	if encrypted == "" {
		return "", ErrMissing
	}
	codec, err := legacycrypto.New(r.secret, legacycrypto.Apify)
	if err != nil {
		return "", ErrDecrypt
	}
	token, err := codec.Decrypt(encrypted)
	if err != nil || !keyPattern.MatchString(token) {
		return "", ErrDecrypt
	}
	if tx.Commit() != nil {
		return "", ErrStorage
	}
	return token, nil
}
