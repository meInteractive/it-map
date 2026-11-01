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
	rows, err := tx.QueryContext(ctx, "SELECT c.id,c.company_name FROM company_identity c WHERE ?=0 OR EXISTS (SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1) ORDER BY c.id", favorites, userName)
	if err != nil {
		return nil, ErrStorage
	}
	type owner struct {
		id   int64
		name string
	}
	owners := []owner{}
	for rows.Next() {
		var o owner
		if rows.Scan(&o.id, &o.name) != nil {
			rows.Close()
			return nil, ErrStorage
		}
		owners = append(owners, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, ErrStorage
	}
	out := []NamedHHSource{}
	for _, o := range owners {
		source, _, e := readHHSource(ctx, tx, o.id, false)
		if errors.Is(e, ErrSourceMissing) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, NamedHHSource{HHSource: source, CompanyName: o.name})
	}
	if tx.Commit() != nil {
		return nil, ErrStorage
	}
	return out, nil
}
