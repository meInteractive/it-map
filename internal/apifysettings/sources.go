package apifysettings

import (
	"context"
	"database/sql"
	"errors"
)

type NamedHHSource struct {
	HHSource
	CompanyName string `json:"companyName"`
}

// Список предназначен только для HH организаций; область избранного читается из общих настроек.
func (r Repository) ListHHSources(ctx context.Context, userName string) ([]NamedHHSource, error) {
	if r.DB == nil {
		return nil, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, ErrStorage
	}
	defer tx.Rollback()
	favorites := int64(1)
	if err = tx.QueryRowContext(ctx, "SELECT favorites_only FROM firecrawl_settings WHERE id=1").Scan(&favorites); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrStorage
	}
	if favorites != 0 && favorites != 1 {
		return nil, ErrStorage
	}
	var catalog []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalog); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrStorage
	}
	urls, err := catalogHHSourceURLs(catalog)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.company_name,COALESCE(s.source_url,''),
		COALESCE(s.enabled,0),COALESCE(s.revision,0),COALESCE(s.auto_enabled,0),s.config_json
		FROM company_identity c LEFT JOIN vacancy_source_parsing s ON s.company_id=c.id AND s.source_id='hh'
		WHERE ?=0 OR EXISTS (SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1)
		ORDER BY c.id`, favorites, userName)
	if err != nil {
		return nil, ErrStorage
	}
	defer rows.Close()
	out := []NamedHHSource{}
	for rows.Next() {
		var id, enabled, revision, auto int64
		var name, storedURL string
		var config []byte
		if err := rows.Scan(&id, &name, &storedURL, &enabled, &revision, &auto, &config); err != nil {
			return nil, ErrStorage
		}
		if url := urls[name]; url != "" {
			source, err := hhSourceView(id, url, storedURL, enabled, revision, auto, config)
			if err != nil {
				return nil, err
			}
			out = append(out, NamedHHSource{HHSource: source, CompanyName: name})
		}
	}
	if rows.Err() != nil {
		return nil, ErrStorage
	}
	if tx.Commit() != nil {
		return nil, ErrStorage
	}
	return out, nil
}
