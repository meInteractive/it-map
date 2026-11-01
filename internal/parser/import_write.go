package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrImportBlocked = errors.New("vacancy import needs manual resolution")

type importMain struct {
	ID                                              int64
	Company, Entity, URL, Title, Description, Level string
	SalaryFrom, SalaryTo                            sql.NullInt64
	Published                                       sql.NullString
}

func nullableString(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}
func nullableSalary(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}
func ptrSalary(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
func importNonempty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func writeHHImport(ctx context.Context, tx *sql.Tx, j Job, company, url, canonical string, namespace, external sql.NullString, s leafSnapshot, leaf *leafCache) (HHImportResult, error) {
	target, keys, err := readImportTarget(ctx, tx, *j.CandidateID, canonical)
	if err != nil {
		return HHImportResult{}, err
	}
	if err = checkImportCatalog(ctx, tx, company); err != nil {
		return HHImportResult{}, err
	}
	normalized, err := NormalizeExtraction(s.ExtractCache.Data, s.Extraction.Snapshot.Markdown)
	if err != nil || len(normalized.Corrections) != 0 {
		return HHImportResult{}, ErrSnapshotInvalid
	}
	input, err := mapImportExtraction(company, url, s.Extraction.Snapshot.FetchedAt, leaf.IDs, normalized.Data)
	if err != nil {
		return HHImportResult{}, err
	}
	if target == 0 {
		target, err = readImportContentTarget(ctx, tx, company, canonical, input.Title, input.DescriptionMD)
		if err != nil {
			return HHImportResult{}, err
		}
	}
	var main importMain
	existing := target != 0
	if existing {
		err = tx.QueryRowContext(ctx, "SELECT id,company_name,entity_type,url,title,description_md,salary_from,salary_to,level,published_at FROM company_vacancies WHERE id=? FOR UPDATE", target).Scan(&main.ID, &main.Company, &main.Entity, &main.URL, &main.Title, &main.Description, &main.SalaryFrom, &main.SalaryTo, &main.Level, &main.Published)
		if err != nil || main.Company != company || main.Entity != "company" {
			return HHImportResult{}, ErrImportIdentityConflict
		}
	}
	fields := []string{"url", "title", "description_md", "salary_from", "salary_to", "level", "published_at"}
	incoming := []any{input.URL, input.Title, input.DescriptionMD, ptrSalary(input.SalaryFrom), ptrSalary(input.SalaryTo), importNonempty(input.Level), importNonempty(input.PublishedAt)}
	current := []any{nil, nil, nil, nil, nil, nil, nil}
	if existing {
		current = []any{main.URL, main.Title, main.Description, nullableSalary(main.SalaryFrom), nullableSalary(main.SalaryTo), main.Level, nullableString(main.Published)}
	}
	plans := make([]importFieldPlan, len(fields))
	for i, field := range fields {
		var ledger string
		var manual int
		if existing {
			err = tx.QueryRowContext(ctx, "SELECT value_json,manual FROM vacancy_parser_fields WHERE vacancy_id=? AND field=? FOR UPDATE", target, field).Scan(&ledger, &manual)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return HHImportResult{}, ErrSettingsStorage
			}
		}
		plans[i] = planImportField(current[i], incoming[i], existing, manual != 0, ledger)
	}
	plans[3], plans[4] = protectImportSalary(plans[3], plans[4], current[3], current[4])
	if _, err = tx.ExecContext(ctx, "INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)", company); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	action := "create"
	if !existing {
		res, e := tx.ExecContext(ctx, "INSERT INTO company_vacancies(company_name,entity_type,url,title,description_md,salary_from,salary_to,level,published_at,loaded_at,role) VALUES(?,'company',?,?,?,?,?,?,?,?, '')", company, input.URL, input.Title, input.DescriptionMD, ptrSalary(input.SalaryFrom), ptrSalary(input.SalaryTo), input.Level, importNonempty(input.PublishedAt), input.LoadedAt)
		if e != nil {
			return HHImportResult{}, ErrSettingsStorage
		}
		target, e = res.LastInsertId()
		if e != nil || target <= 0 {
			return HHImportResult{}, ErrSettingsStorage
		}
	} else {
		action = "unchanged"
	}
	token, err := stageToken()
	if err != nil {
		return HHImportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_import_write_context(vacancy_id,generation,owner_token) VALUES(?,?,?)", target, j.Generation, token); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	changed := !existing
	if existing {
		for _, p := range plans {
			changed = changed || p.Changed
		}
		if changed {
			_, err = tx.ExecContext(ctx, "UPDATE company_vacancies SET url=?,title=?,description_md=?,salary_from=?,salary_to=?,level=?,published_at=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", plans[0].Value, plans[1].Value, plans[2].Value, plans[3].Value, plans[4].Value, plans[5].Value, plans[6].Value, target)
			if err != nil {
				return HHImportResult{}, ErrSettingsStorage
			}
		}
	}
	rolesChanged, err := writeImportRoles(ctx, tx, target, *j.CandidateID, leaf.IDs)
	if err != nil {
		return HHImportResult{}, err
	}
	changed = changed || rolesChanged
	var oldTarget int64
	var oldExtraction sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT vacancy_id,extraction_id FROM vacancy_publication_sources WHERE candidate_id=? FOR UPDATE", j.CandidateID).Scan(&oldTarget, &oldExtraction)
	if err == nil && oldTarget != target {
		return HHImportResult{}, ErrImportIdentityConflict
	}
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!oldExtraction.Valid || oldExtraction.String != s.ExtractCache.ID) {
		changed = true
	} else if err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	for _, key := range keys {
		var known int64
		err = tx.QueryRowContext(ctx, "SELECT vacancy_id FROM vacancy_publication_keys WHERE identity_key=? FOR UPDATE", key).Scan(&known)
		if errors.Is(err, sql.ErrNoRows) {
			changed = true
			_, err = tx.ExecContext(ctx, "INSERT INTO vacancy_publication_keys(identity_key,vacancy_id) VALUES(?,?)", key, target)
		} else if err == nil && known != target {
			return HHImportResult{}, ErrImportIdentityConflict
		}
		if err != nil {
			return HHImportResult{}, ErrSettingsStorage
		}
	}
	if changed {
		for i, field := range fields {
			if plans[i].Managed && !importEmpty(incoming[i]) {
				if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_parser_fields(vacancy_id,field,value_json,candidate_id) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE value_json=IF(manual=0,VALUES(value_json),value_json),candidate_id=IF(manual=0,VALUES(candidate_id),candidate_id)", target, field, importValueJSON(plans[i].Value), j.CandidateID); err != nil {
					return HHImportResult{}, ErrSettingsStorage
				}
			}
		}
		metadata := map[string]any{}
		for k, v := range normalized.Data {
			switch k {
			case "title", "description_md", "salary_from", "salary_to", "level", "published_at":
			default:
				metadata[k] = v
			}
		}
		if input.Level == "" && normalized.Data["level"] != nil {
			metadata["level"] = normalized.Data["level"]
		}
		if normalized.Data["salary_currency"] != "RUB" || normalized.Data["salary_period"] != "month" {
			metadata["salary_from"] = normalized.Data["salary_from"]
			metadata["salary_to"] = normalized.Data["salary_to"]
		}
		raw, err := json.Marshal(metadata)
		if err != nil {
			return HHImportResult{}, ErrSettingsStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_parser_fields(vacancy_id,field,value_json,candidate_id) VALUES(?,'source_metadata',?,?) ON DUPLICATE KEY UPDATE value_json=VALUES(value_json),candidate_id=VALUES(candidate_id)", target, string(raw), j.CandidateID); err != nil {
			return HHImportResult{}, ErrSettingsStorage
		}
	}
	if changed {
		_, err = tx.ExecContext(ctx, "INSERT INTO vacancy_publication_sources(candidate_id,vacancy_id,url,canonical_url,source_namespace,external_job_id,extraction_id,updated_at) VALUES(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE url=VALUES(url),canonical_url=VALUES(canonical_url),extraction_id=VALUES(extraction_id),updated_at=VALUES(updated_at)", j.CandidateID, target, url, canonical, namespace, external, s.ExtractCache.ID, at)
		if err != nil {
			return HHImportResult{}, ErrSettingsStorage
		}
	}
	if existing && changed {
		action = "update"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vacancy_candidates SET main_vacancy_id=?,updated_at=? WHERE id=?", target, at, j.CandidateID); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO company_vacancy_state(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)", company); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_import_write_context WHERE vacancy_id=? AND owner_token=?", target, token); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	receipt := importReceipt{Generation: j.Generation, DedupeKey: j.DedupeKey, LeafInputHash: s.InputHash, SnapshotID: s.Extraction.Snapshot.ID, ExtractionID: s.ExtractCache.ID, RoleResultID: leaf.ID, Fields: []string{}, ProtectedFields: []string{}, RoleIDs: []int64{}}
	for i, field := range fields {
		if plans[i].Changed {
			receipt.Fields = append(receipt.Fields, field)
		} else if existing && !plans[i].Managed && !importEmpty(incoming[i]) && current[i] != incoming[i] {
			receipt.ProtectedFields = append(receipt.ProtectedFields, field)
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=? ORDER BY role_id FOR UPDATE", target)
	if err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		receipt.RoleIDs = append(receipt.RoleIDs, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,action,status,processed_at,input_hash,changes_json) VALUES(?,?,?,?,'success',?,?,?)", j.ID, j.CandidateID, target, action, at, s.InputHash, string(receiptBytes)); err != nil {
		return HHImportResult{}, ErrSettingsStorage
	}
	return HHImportResult{*j.CandidateID, target, action, false}, nil
}

// Каталог старых вакансий должен быть перенесён до первого импорта компании.
func checkImportCatalog(ctx context.Context, tx *sql.Tx, company string) error {
	var data []byte
	if tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1 FOR UPDATE").Scan(&data) != nil {
		return ErrCandidateChanged
	}
	var catalog struct {
		Companies []struct {
			Name string                        `json:"name"`
			Jobs []struct{ Title, URL string } `json:"jobs"`
		} `json:"companies"`
	}
	if json.Unmarshal(data, &catalog) != nil || catalog.Companies == nil {
		return ErrCandidateChanged
	}
	var found bool
	var jobs bool
	for _, c := range catalog.Companies {
		if c.Name == company {
			if found {
				return ErrCandidateChanged
			}
			found = true
			for _, job := range c.Jobs {
				if strings.TrimSpace(job.Title) != "" && strings.TrimSpace(job.URL) != "" {
					jobs = true
				}
			}
		}
	}
	if !found {
		return ErrCandidateChanged
	}
	if jobs {
		var name string
		err := tx.QueryRowContext(ctx, "SELECT company_name FROM company_vacancy_state WHERE company_name=? FOR UPDATE", company).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrImportBlocked
		}
		if err != nil {
			return ErrSettingsStorage
		}
	}
	return nil
}

func writeImportRoles(ctx context.Context, tx *sql.Tx, target, candidate int64, auto []int64) (bool, error) {
	catalog, err := leafRoleCatalog(ctx, tx)
	if err != nil {
		return false, err
	}
	roleNames := map[int64]string{}
	for _, r := range catalog.Leaves {
		roleNames[r.ID] = r.Name
	}
	old := map[int64]bool{}
	owned := map[int64]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=? ORDER BY role_id FOR UPDATE", target)
	if err != nil {
		return false, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		old[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, ErrSettingsStorage
	}
	rows, err = tx.QueryContext(ctx, "SELECT role_id FROM vacancy_parser_roles WHERE vacancy_id=? FOR UPDATE", target)
	if err != nil {
		return false, ErrSettingsStorage
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		owned[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, ErrSettingsStorage
	}
	automatic := map[int64]bool{}
	for _, id := range auto {
		if _, ok := roleNames[id]; !ok {
			return false, ErrCandidateChanged
		}
		automatic[id] = true
	}
	if len(automatic) == 0 {
		return false, ErrCandidateChanged
	}
	result := map[int64]bool{}
	for id := range old {
		if !owned[id] || automatic[id] {
			result[id] = true
		}
	}
	for id := range automatic {
		result[id] = true
	}
	if len(result) > 12 {
		return false, ErrImportBlocked
	}
	for id := range result {
		if _, ok := roleNames[id]; !ok {
			return false, ErrImportBlocked
		}
	}
	changed := len(result) != len(old)
	if !changed {
		for id := range result {
			if !old[id] {
				changed = true
				break
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_parser_roles WHERE vacancy_id=? AND candidate_id=?", target, candidate); err != nil {
		return false, ErrSettingsStorage
	}
	for id := range old {
		if !result[id] {
			if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_parser_roles WHERE vacancy_id=? AND role_id=?", target, id); err != nil {
				return false, ErrSettingsStorage
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_role_assignments WHERE vacancy_id=? AND role_id=?", target, id); err != nil {
				return false, ErrSettingsStorage
			}
		}
	}
	for id := range automatic {
		if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?) ON DUPLICATE KEY UPDATE role_id=VALUES(role_id)", target, id); err != nil {
			return false, ErrSettingsStorage
		}
		if !old[id] || owned[id] {
			if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_parser_roles(vacancy_id,role_id,candidate_id) VALUES(?,?,?) ON DUPLICATE KEY UPDATE role_id=VALUES(role_id)", target, id, candidate); err != nil {
				return false, ErrSettingsStorage
			}
		}
	}
	if changed {
		ids := make([]int64, 0, len(result))
		for id := range result {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			names = append(names, roleNames[id])
		}
		if _, err = tx.ExecContext(ctx, "UPDATE company_vacancies SET role=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", strings.Join(names, "; "), target); err != nil {
			return false, ErrSettingsStorage
		}
	}
	return changed, nil
}
