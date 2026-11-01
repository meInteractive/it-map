package parser

import (
	"context"
	"database/sql"
	"errors"
)

var ErrImportIdentityConflict = errors.New("vacancy publication identities conflict")

// readImportTarget блокирует существующие точные связи и запрещает их слияние,
// если они указывают на разные карточки. Заголовок не является идентификатором.
// Вызывается под общим import:global lease и внутри owned-транзакции.
func readImportTarget(ctx context.Context, tx *sql.Tx, candidateID int64, canonical string) (int64, []string, error) {
	identity := importHHIdentity(canonical)
	if identity == "" {
		return 0, nil, ErrCandidateChanged
	}
	keys := []string{"url:" + stageHash(canonical)}
	rows, err := tx.QueryContext(ctx, "SELECT identity_key FROM vacancy_candidate_identities WHERE candidate_id=? ORDER BY identity_key FOR UPDATE", candidateID)
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	seen := map[string]bool{keys[0]: true}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			break
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	targets := map[int64]bool{}
	for _, key := range keys {
		var id int64
		err = tx.QueryRowContext(ctx, "SELECT vacancy_id FROM vacancy_publication_keys WHERE identity_key=? FOR UPDATE", key).Scan(&id)
		if err == nil {
			targets[id] = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, nil, ErrSettingsStorage
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,url FROM company_vacancies FOR UPDATE")
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		var url string
		if err = rows.Scan(&id, &url); err != nil {
			break
		}
		if url == canonical || importHHIdentity(url) == identity {
			targets[id] = true
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	rows, err = tx.QueryContext(ctx, "SELECT vacancy_id,canonical_url FROM vacancy_publication_sources FOR UPDATE")
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		var url string
		if err = rows.Scan(&id, &url); err != nil {
			break
		}
		if url == canonical || importHHIdentity(url) == identity {
			targets[id] = true
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, nil, ErrSettingsStorage
	}
	var linked sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE", candidateID).Scan(&linked); err != nil {
		return 0, nil, ErrSettingsStorage
	}
	if linked.Valid {
		targets[linked.Int64] = true
	}
	if len(targets) > 1 {
		return 0, nil, ErrImportIdentityConflict
	}
	for id := range targets {
		if id <= 0 {
			return 0, nil, ErrImportIdentityConflict
		}
		return id, keys, nil
	}
	return 0, keys, nil
}
