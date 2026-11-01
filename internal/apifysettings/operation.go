package apifysettings

import (
	"context"
	"database/sql"
	"errors"
	"itmap/internal/integrations/apify"
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
	CandidateID      int64  `json:",omitempty"`
	TitleRevision    int64  `json:",omitempty"`
	VacancyURL       string `json:",omitempty"`
}

func validateOperation(v OperationVersion, generation string, settings StoredSettings, source HHSource) error {
	if v.CandidateID != 0 || v.TitleRevision != 0 || v.VacancyURL != "" {
		if v.CandidateID < 1 || v.CandidateID > 9007199254740991 || v.TitleRevision < 1 || v.TitleRevision > 9007199254740991 || len(v.VacancyURL) > 2048 {
			return ErrInput
		}
		if _, err := apify.HHInput(v.VacancyURL, apify.HHOptions{Detail: true}); err != nil {
			return ErrInput
		}
	}
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
	token, err := r.resolveOperationTx(ctx, tx, v, false)
	if err != nil {
		return "", err
	}
	if tx.Commit() != nil {
		return "", ErrStorage
	}
	return token, nil
}

// Вызывающий денежный журнал удерживает блокировку поколения и настроек
// до фиксации резерва; сетевые операции внутри этой транзакции запрещены.
func (r Repository) ResolveOperationTx(ctx context.Context, tx *sql.Tx, v OperationVersion) (string, error) {
	return r.resolveOperationTx(ctx, tx, v, true)
}

func (r Repository) resolveOperationTx(ctx context.Context, tx *sql.Tx, v OperationVersion, lock bool) (string, error) {
	if tx == nil {
		return "", ErrStorage
	}
	var generation string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation) != nil {
		return "", ErrStorage
	}
	s, err := readStored(ctx, tx, lock)
	if err != nil {
		return "", err
	}
	source, _, err := readHHSource(ctx, tx, v.CompanyID, lock)
	if err != nil {
		return "", err
	}
	if err = validateOperation(v, generation, s, source); err != nil {
		return "", err
	}
	if v.CandidateID != 0 {
		query := "SELECT company_id,agency_id,source_id,vacancy_url,title_revision,main_vacancy_id FROM vacancy_candidates WHERE id=?"
		if lock {
			query += " FOR UPDATE"
		}
		var company, agency, main sql.NullInt64
		var source, url sql.NullString
		var revision int64
		err := tx.QueryRowContext(ctx, query, v.CandidateID).Scan(&company, &agency, &source, &url, &revision, &main)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrChanged
		}
		if err != nil {
			return "", ErrStorage
		}
		if !company.Valid || company.Int64 != v.CompanyID || agency.Valid || main.Valid || !source.Valid || source.String != "hh" || !url.Valid || url.String != v.VacancyURL || revision != v.TitleRevision {
			return "", ErrChanged
		}
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
	return token, nil
}
