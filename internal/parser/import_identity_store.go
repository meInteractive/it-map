package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"errors"
	whatwg "github.com/nlnwa/whatwg-url/url"
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

type importContentRow struct {
	id               int64
	url, title, body string
}

// readImportContentTarget ищет подтверждённое совпадение между разными
// источниками и запрещает выбор при нескольких подходящих карточках.
func readImportContentTarget(ctx context.Context, tx *sql.Tx, company, canonical, title, body string) (int64, error) {
	incomingID := importHHIdentity(canonical)
	if incomingID == "" {
		return 0, ErrCandidateChanged
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,url,title,description_md FROM company_vacancies WHERE company_name=? AND entity_type='company' FOR UPDATE", company)
	if err != nil {
		return 0, ErrSettingsStorage
	}
	var entries []importContentRow
	for rows.Next() {
		var r importContentRow
		if err = rows.Scan(&r.id, &r.url, &r.title, &r.body); err != nil {
			break
		}
		entries = append(entries, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, ErrSettingsStorage
	}
	rows, err = tx.QueryContext(ctx, "SELECT s.vacancy_id,s.canonical_url,e.extracted_data FROM vacancy_publication_sources s JOIN vacancy_extractions e ON e.id=s.extraction_id JOIN company_vacancies v ON v.id=s.vacancy_id WHERE v.company_name=? AND v.entity_type='company' FOR UPDATE", company)
	if err != nil {
		return 0, ErrSettingsStorage
	}
	for rows.Next() {
		var r importContentRow
		var raw []byte
		if err = rows.Scan(&r.id, &r.url, &raw); err != nil {
			break
		}
		var data struct {
			Title       string `json:"title"`
			Description string `json:"description_md"`
		}
		if json.Unmarshal(raw, &data) == nil {
			r.title = data.Title
			r.body = data.Description
		}
		entries = append(entries, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, ErrSettingsStorage
	}
	distinct := map[int64]bool{}
	for _, r := range entries {
		old := importHHIdentity(r.url)
		if old != "" && old != incomingID {
			distinct[r.id] = true
			continue
		}
		a, ae := whatwg.Parse(r.url)
		b, be := whatwg.Parse(canonical)
		if ae == nil && be == nil && strings.TrimPrefix(a.Hostname(), "www.") == strings.TrimPrefix(b.Hostname(), "www.") && r.url != canonical && old == "" {
			distinct[r.id] = true
		}
	}
	confirmed := map[int64]bool{}
	for _, r := range entries {
		if !distinct[r.id] && importSameContent(r.url, r.title, r.body, canonical, title, body) {
			confirmed[r.id] = true
		}
	}
	if len(confirmed) > 1 {
		return 0, ErrImportIdentityConflict
	}
	for id := range confirmed {
		return id, nil
	}
	return 0, nil
}
