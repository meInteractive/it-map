package apifysettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"itmap/internal/backup"
	"itmap/internal/integrations/apify"
)

var ErrSourceMissing = errors.New("источник HH организации не найден")

// Источник берётся из текущего каталога организации, а не из тела запроса.
func catalogHHSource(data []byte, name string) (string, error) {
	if name == "" {
		return "", ErrSourceMissing
	}
	if len(data) > 0 {
		var catalog struct {
			Companies []struct {
				Name string `json:"name"`
			} `json:"companies"`
		}
		if json.Unmarshal(data, &catalog) != nil || catalog.Companies == nil {
			return "", ErrStorage
		}
		count := 0
		for _, company := range catalog.Companies {
			if company.Name == name {
				count++
			}
		}
		if count != 1 {
			return "", ErrSourceMissing
		}
	}
	detail, err := backup.PrepareCatalogRead(data, backup.CatalogReadState{}, backup.CatalogReadOptions{EntityType: "company", View: "detail", Name: name})
	if err != nil {
		return "", ErrStorage
	}
	var entity *struct {
		VacancySources map[string]json.RawMessage `json:"vacancySources"`
	}
	if json.Unmarshal(detail, &entity) != nil || entity == nil {
		return "", ErrSourceMissing
	}
	var url string
	if json.Unmarshal(entity.VacancySources["hh"], &url) != nil || url == "" {
		return "", ErrSourceMissing
	}
	return url, nil
}

func validateHHSource(catalogURL, submittedURL string, enabled bool) error {
	if catalogURL != submittedURL {
		return ErrChanged
	}
	if enabled {
		if _, err := apify.HHInput(catalogURL, apify.HHOptions{}); err != nil {
			return ErrInput
		}
	}
	return nil
}

type HHSourceInput struct {
	SourceURL string
	Enabled   bool
	Revision  int64
}

func ParseHHSource(body []byte) (HHSourceInput, error) {
	var out HHSourceInput
	f, err := inputObject(body, "sourceUrl", "enabled", "revision")
	if err != nil {
		return out, err
	}
	if json.Unmarshal(f["sourceUrl"], &out.SourceURL) != nil || out.SourceURL == "" || len(out.SourceURL) > 4096 || len(f["enabled"]) == 0 || string(f["enabled"]) == "null" || json.Unmarshal(f["enabled"], &out.Enabled) != nil {
		return HHSourceInput{}, ErrInput
	}
	out.Revision, err = revision(f["revision"])
	if err != nil {
		return HHSourceInput{}, err
	}
	return out, nil
}

type HHSource struct {
	CompanyID   int64  `json:"companyId"`
	SourceID    string `json:"sourceId"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	Supported   bool   `json:"supported"`
	Revision    int64  `json:"revision"`
	AutoEnabled bool   `json:"autoEnabled"`
}

func readHHSource(ctx context.Context, tx *sql.Tx, companyID int64, lock bool) (HHSource, bool, error) {
	if companyID < 1 || companyID > 9007199254740991 {
		return HHSource{}, false, ErrInput
	}
	var name string
	query := "SELECT company_name FROM company_identity WHERE id=?"
	if lock {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, query, companyID).Scan(&name); errors.Is(err, sql.ErrNoRows) {
		return HHSource{}, false, ErrSourceMissing
	} else if err != nil {
		return HHSource{}, false, ErrStorage
	}
	var catalog []byte
	query = "SELECT data_json FROM app_catalog WHERE id=1"
	if lock {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, query).Scan(&catalog); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HHSource{}, false, ErrStorage
	}
	url, err := catalogHHSource(catalog, name)
	if err != nil {
		return HHSource{}, false, err
	}
	out := HHSource{CompanyID: companyID, SourceID: "hh", URL: url}
	_, err = apify.HHInput(url, apify.HHOptions{})
	out.Supported = err == nil
	var storedURL string
	var enabled, auto int64
	query = "SELECT source_url,enabled,revision,auto_enabled FROM vacancy_source_parsing WHERE company_id=? AND source_id='hh'"
	if lock {
		query += " FOR UPDATE"
	}
	err = tx.QueryRowContext(ctx, query, companyID).Scan(&storedURL, &enabled, &out.Revision, &auto)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil || (enabled != 0 && enabled != 1) || (auto != 0 && auto != 1) {
		return HHSource{}, false, ErrStorage
	}
	out.Enabled = storedURL == url && enabled == 1 && out.Supported
	out.AutoEnabled = storedURL == url && auto == 1 && out.Enabled
	return out, true, nil
}

func (r Repository) ReadHHSource(ctx context.Context, companyID int64) (HHSource, error) {
	if r.DB == nil {
		return HHSource{}, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return HHSource{}, ErrStorage
	}
	defer tx.Rollback()
	out, _, err := readHHSource(ctx, tx, companyID, false)
	if err != nil {
		return HHSource{}, err
	}
	if tx.Commit() != nil {
		return HHSource{}, ErrStorage
	}
	return out, nil
}

func (r Repository) SaveHHSource(ctx context.Context, generation string, companyID int64, in HHSourceInput) (HHSource, error) {
	if r.DB == nil {
		return HHSource{}, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return HHSource{}, ErrStorage
	}
	defer tx.Rollback()
	var actual string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual) != nil {
		return HHSource{}, ErrStorage
	}
	if actual != generation || len(actual) != 36 {
		return HHSource{}, ErrGeneration
	}
	current, exists, err := readHHSource(ctx, tx, companyID, true)
	if err != nil {
		return HHSource{}, err
	}
	if current.Revision != in.Revision {
		return HHSource{}, ErrChanged
	}
	if err = validateHHSource(current.URL, in.SourceURL, in.Enabled); err != nil {
		return HHSource{}, err
	}
	if !exists {
		_, err = tx.ExecContext(ctx, "INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled,config_json,revision) VALUES(?,'hh',?,?, '{}',1)", companyID, current.URL, in.Enabled)
	} else {
		// MariaDB вычисляет SET слева направо: сравнения должны предшествовать изменению source_url.
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE vacancy_source_parsing SET auto_enabled=CASE WHEN source_url=? THEN auto_enabled ELSE 0 END,next_run_at=CASE WHEN source_url=? THEN next_run_at ELSE NULL END,source_url=?,enabled=?,revision=revision+1,last_error='',last_success_at=NULL WHERE company_id=? AND source_id='hh' AND revision=?", current.URL, current.URL, current.URL, in.Enabled, companyID, in.Revision)
		if err == nil {
			if n, e := result.RowsAffected(); e != nil || n != 1 {
				return HHSource{}, ErrChanged
			}
		}
	}
	if err != nil {
		return HHSource{}, ErrStorage
	}
	out, _, err := readHHSource(ctx, tx, companyID, false)
	if err != nil {
		return HHSource{}, err
	}
	if tx.Commit() != nil {
		return HHSource{}, ErrStorage
	}
	return out, nil
}
