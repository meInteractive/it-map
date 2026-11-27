package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type importCatalogEntity struct {
	Name string `json:"name"`
}

func readVacancyImportOwners(ctx context.Context, tx *sql.Tx) ([]byte, vacancyImportOwners, error) {
	var data []byte
	owners := vacancyImportOwners{map[int64]string{}, map[int64]string{}, map[string]bool{}, map[string]bool{}}
	if err := tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil {
		return nil, owners, err
	}
	var catalog struct{ Companies, Agencies []importCatalogEntity }
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, owners, err
	}
	for _, source := range []struct {
		query    string
		entities []importCatalogEntity
		names    map[string]bool
		ids      map[int64]string
	}{
		{"SELECT id,company_name FROM company_identity", catalog.Companies, owners.companyNames, owners.companies},
		{"SELECT id,agency_name FROM agency_identity", catalog.Agencies, owners.agencyNames, owners.agencies},
	} {
		for _, entity := range source.entities {
			source.names[entity.Name] = true
		}
		rows, err := tx.QueryContext(ctx, source.query)
		if err != nil {
			return nil, owners, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err = rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, owners, err
			}
			if source.names[name] {
				source.ids[id] = name
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, owners, err
		}
	}
	return data, owners, nil
}

func readImportedVacancies(ctx context.Context, tx *sql.Tx) (map[string]importedVacancy, map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,company_name,entity_type,url FROM company_vacancies")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	existing := map[string]importedVacancy{}
	ambiguous := map[string]bool{}
	for rows.Next() {
		var item importedVacancy
		var link string
		if err = rows.Scan(&item.id, &item.owner, &item.kind, &link); err != nil {
			return nil, nil, err
		}
		key := vacancyImportURLKey(link)
		if _, ok := existing[key]; ok {
			ambiguous[key] = true
		}
		existing[key] = item
	}
	return existing, ambiguous, rows.Err()
}

func writeImportedVacancy(ctx context.Context, tx *sql.Tx, user string, row vacancyImportRow, previous importedVacancy) error {
	var err error
	id := previous.id
	if id > 0 {
		var owner, kind, link string
		err = tx.QueryRowContext(ctx, `SELECT company_name,entity_type,url,cover_letter_subject,cover_letter_md
			FROM company_vacancies WHERE id=? FOR UPDATE`, id).Scan(&owner, &kind, &link, &previous.subject, &previous.letter)
		if err != nil {
			return err
		}
		if owner != row.CompanyName || kind != row.EntityType || vacancyImportURLKey(link) != row.URL {
			return ErrWriteBusy
		}
		if !row.HasSubject {
			row.CoverLetterSubject = previous.subject
		}
		if !row.HasLetter {
			row.CoverLetterMD = previous.letter
		}
		_, err = tx.ExecContext(ctx, `UPDATE company_vacancies SET real_company_name=?,url=?,salary_from=?,salary_to=?,
			title=?,role=?,level=?,published_at=?,loaded_at=?,description_md=?,cover_letter_subject=?,cover_letter_md=?,
			updated_at=CURRENT_TIMESTAMP WHERE id=?`, nullText(row.RealCompanyName), row.URL, row.SalaryFrom, row.SalaryTo, row.Title, row.Role,
			row.Level, nullText(row.PublishedAt), nullText(row.LoadedAt), row.DescriptionMD, row.CoverLetterSubject, row.CoverLetterMD, id)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM vacancy_role_assignments WHERE vacancy_id=?", id); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)`, row.CompanyName); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO company_vacancies
			(company_name,entity_type,real_company_name,url,salary_from,salary_to,title,role,level,
			 published_at,loaded_at,description_md,cover_letter_subject,cover_letter_md)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.CompanyName, row.EntityType, nullText(row.RealCompanyName), row.URL,
			row.SalaryFrom, row.SalaryTo, row.Title, row.Role, row.Level, nullText(row.PublishedAt), nullText(row.LoadedAt),
			row.DescriptionMD, row.CoverLetterSubject, row.CoverLetterMD)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if id <= 0 {
			return errors.New("vacancy insert did not return an ID")
		}
	}
	for _, roleID := range row.RoleIDs {
		var leaf int64
		if err = tx.QueryRowContext(ctx, `SELECT r.id FROM vacancy_roles r WHERE r.id=? AND NOT EXISTS
			(SELECT 1 FROM vacancy_roles child WHERE child.parent_id=r.id) FOR UPDATE`, roleID).Scan(&leaf); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)", id, roleID); err != nil {
			return err
		}
	}
	return saveUserRelevance(ctx, tx, user, id, row.Relevance)
}

func saveUserRelevance(ctx context.Context, tx *sql.Tx, user string, id int64, relevance *int64) error {
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=?`, user, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		_, err := tx.ExecContext(ctx, `UPDATE company_vacancy_user_state SET relevance=?,updated_at=CURRENT_TIMESTAMP WHERE user_name=? AND vacancy_id=?`, relevance, user, id)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at) VALUES(?,?,?,CURRENT_TIMESTAMP)`, user, id, relevance)
	return err
}

func refreshImportedAgencySalaries(ctx context.Context, tx *sql.Tx, data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	var agencies []map[string]json.RawMessage
	if raw := root["agencies"]; len(raw) == 0 {
		return nil
	} else if err := json.Unmarshal(raw, &agencies); err != nil {
		return err
	}
	type bounds struct{ from, to sql.NullInt64 }
	aggregates := map[string]bounds{}
	rows, err := tx.QueryContext(ctx, `SELECT company_name,LOWER(level),MIN(salary_from),MAX(salary_to) FROM company_vacancies
		WHERE entity_type='agency' AND LOWER(level) IN ('middle','senior','lead','head') GROUP BY company_name,LOWER(level)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name, level string
		var value bounds
		if err = rows.Scan(&name, &level, &value.from, &value.to); err != nil {
			rows.Close()
			return err
		}
		aggregates[name+"\x00"+level] = value
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	changed := false
	for _, agency := range agencies {
		var name string
		if json.Unmarshal(agency["name"], &name) != nil || name == "" {
			continue
		}
		var sources map[string]string
		_ = json.Unmarshal(agency["salaryRangeSource"], &sources)
		if sources == nil {
			sources = map[string]string{}
		}
		for _, level := range []string{"middle", "senior", "lead", "head"} {
			var previous [2]*int64
			_ = json.Unmarshal(agency[level], &previous)
			if sources[level] != "vacancies" && ((previous[0] != nil && *previous[0] != 0) || (previous[1] != nil && *previous[1] != 0)) {
				continue
			}
			value := aggregates[name+"\x00"+level]
			derived := [2]*int64{nullableInt(value.from), nullableInt(value.to)}
			mode := "manual"
			if value.from.Int64 != 0 || value.to.Int64 != 0 {
				mode = "vacancies"
			}
			raw, _ := json.Marshal(derived)
			if strings.TrimSpace(string(agency[level])) == string(raw) && sources[level] == mode {
				continue
			}
			if sources[level] != "vacancies" && mode == "manual" && !value.from.Valid && !value.to.Valid {
				continue
			}
			agency[level] = raw
			sources[level] = mode
			changed = true
		}
		agency["salaryRangeSource"], _ = json.Marshal(sources)
	}
	if !changed {
		return nil
	}
	root["agencies"], err = json.Marshal(agencies)
	if err != nil {
		return err
	}
	root["updated"], _ = json.Marshal(time.Now().UTC().Format("2006-01-02"))
	updated, err := json.Marshal(root)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE app_catalog SET data_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=1", updated)
	return err
}
