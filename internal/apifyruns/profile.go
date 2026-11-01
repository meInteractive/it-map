package apifyruns

import (
	"context"
	"database/sql"
	"errors"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

// ResolveProfile продолжает профиль уже существующей денежной операции. Выбор
// нового профиля сам по себе не резервирует бюджет и не разрешает отправку POST.
func (s Service) ResolveProfile(ctx context.Context, v apifysettings.OperationVersion, key string, options apify.HHOptions) (string, error) {
	r := s.Repository
	if r.DB == nil || r.Settings.DB != r.DB {
		return "", ErrStorage
	}
	cfg, err := r.Settings.Read(ctx)
	if err != nil {
		return "", err
	}
	if cfg.Revision != v.SettingsRevision {
		return "", apifysettings.ErrChanged
	}
	if !cfg.Enabled {
		return "", apifysettings.ErrDisabled
	}
	scoped, err := operationKey(v, key, options)
	if err != nil {
		return "", err
	}
	var profile string
	err = r.DB.QueryRowContext(ctx, "SELECT profile FROM apify_runs WHERE generation=? AND operation_key=?", v.Generation, scoped).Scan(&profile)
	if err == nil {
		if profile != "free" && profile != "starter" {
			return "", ErrStorage
		}
		v.Profile = profile
		if _, err = r.readOperation(ctx, v, scoped); err != nil {
			return "", err
		}
		return profile, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", ErrStorage
	}
	profile = "free"
	if !cfg.Profiles[profile].Configured {
		profile = "starter"
		if !cfg.PaidAllowed || !cfg.Profiles[profile].Configured {
			return "", apifysettings.ErrMissing
		}
	}
	v.Profile = profile
	if _, err = r.Settings.ResolveOperation(ctx, v); err != nil {
		return "", err
	}
	return profile, nil
}
