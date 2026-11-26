package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/backup"
)

type CompanyImport struct {
	Mode                                            string
	Companies, Offices, Reviews, Agencies, Contacts []ImportRow
}

type CompanyImportIssue struct {
	Sheet   string `json:"sheet"`
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type CompanyImportDuplicate struct {
	Row    int    `json:"row"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type CompanyImportResult map[string]any

type ImportError string

func (e ImportError) Error() string { return string(e) }

type importReport struct {
	counts     map[string]int
	issues     []CompanyImportIssue
	duplicates []CompanyImportDuplicate
}

func (r *importReport) issue(sheet string, row ImportRow, index int, err error) {
	if len(r.issues) < 100 {
		r.issues = append(r.issues, CompanyImportIssue{sheet, importRowNumber(row, index), err.Error()})
	}
}

func importRowNumber(row ImportRow, index int) int {
	if id, err := importID(row, "row"); err == nil && id > 0 {
		return int(id)
	}
	return index + 2
}

func (r *importReport) result() CompanyImportResult {
	r.counts["notAdded"] = r.counts["duplicates"] + r.counts["invalid"]
	out := CompanyImportResult{"errors": r.issues, "duplicateDetails": r.duplicates}
	for _, key := range strings.Fields("added updated notAdded duplicates invalid officesAdded officesUpdated officesSkipped reviewsUpdated reviewsSkipped agenciesAdded agenciesUpdated agenciesSkipped contactsAdded contactsUpdated contactsSkipped") {
		out[key] = r.counts[key]
	}
	return out
}

type importOwners struct {
	entities       []storedEntity
	names          map[string]int
	ids, sourceIDs map[int64]int
}

func newImportOwners(entities []storedEntity, identities map[string]int64) *importOwners {
	o := &importOwners{entities: entities, names: map[string]int{}, ids: map[int64]int{}, sourceIDs: map[int64]int{}}
	for i, entity := range entities {
		name := nameOf(entity)
		o.names[importKey(name)] = i
		if id := identities[name]; id > 0 {
			o.ids[id] = i
		}
	}
	return o
}

func (o *importOwners) lookup(id int64, name string, preferSource bool) (int, bool, error) {
	byID, hasID := o.ids[id]
	if idx, exists := o.sourceIDs[id]; preferSource && exists {
		byID, hasID = idx, true
	}
	byName, hasName := o.names[importKey(name)]
	if hasID && hasName && byID != byName {
		return 0, false, fmt.Errorf("ID и название указывают на разные организации")
	}
	if hasName {
		return byName, true, nil
	}
	return byID, hasID, nil
}

func (o *importOwners) importEntities(rows []ImportRow, sheet, idField, mode string, agency bool, report *importReport) {
	used := map[int]bool{}
	sourceKeys := map[string]string{}
	indexSources := func(entity storedEntity) {
		for _, field := range []string{"companySite", "hh", "habr"} {
			if key := importURLKey(asText(obj(entity["vacancySources"])[field])); key != "" {
				sourceKeys[field+"\x00"+key] = nameOf(entity)
			}
		}
	}
	for _, entity := range o.entities {
		indexSources(entity)
	}
	for i, row := range rows {
		added, updated, skipped := "added", "updated", "invalid"
		if agency {
			added, updated, skipped = "agenciesAdded", "agenciesUpdated", "agenciesSkipped"
		}
		id, err := importID(row, idField)
		name := importText(row, "name")
		index, exists, lookupErr := o.lookup(id, name, false)
		if mode == "create" {
			index, exists, lookupErr = o.lookup(0, name, false)
			if agency && !exists && id > 0 {
				index, exists = o.ids[id]
			}
		}
		if err == nil {
			err = lookupErr
		}
		if err == nil && mode == "create" {
			reason := ""
			if exists {
				reason = "организация с таким названием уже существует"
			}
			if !agency && reason == "" {
				for _, field := range []string{"companySite", "hh", "habr"} {
					if owner := sourceKeys[field+"\x00"+importURLKey(importText(row, field))]; owner != "" {
						reason = "источник " + field + " уже указан у компании «" + owner + "»"
						break
					}
				}
			}
			if reason != "" {
				if agency {
					report.counts[skipped]++
				} else {
					report.counts["duplicates"]++
					if len(report.duplicates) < 50 {
						report.duplicates = append(report.duplicates, CompanyImportDuplicate{importRowNumber(row, i), name, reason})
					}
				}
				continue
			}
			if _, reused := o.sourceIDs[id]; id > 0 && reused {
				err = fmt.Errorf("ID уже указан в другой новой строке")
			}
			if name == "" || utf8.RuneCountInString(name) > 255 {
				err = fmt.Errorf("укажите название длиной до 255 символов")
			}
		}
		if err == nil && mode == "update" && (!exists || used[index]) {
			err = fmt.Errorf("организация не найдена или уже обновлена другой строкой")
		}
		if err == nil && mode == "update" && agency && name != "" && importKey(name) != importKey(nameOf(o.entities[index])) {
			err = fmt.Errorf("название не соответствует ID агентства")
		}
		if err == nil {
			entity := storedEntity{"name": name, "offices": []any{}, "jobs": []any{}}
			if !agency {
				entity["ratingFormulaVersion"] = 5
			}
			if mode == "update" {
				entity = maps.Clone(o.entities[index])
			}
			sets := importCompanyUpdates
			if agency {
				sets = append(append([][]importField{}, importAgencyFields...), importEntityCreate...)
				sets = append(sets, importRevenue...)
			}
			if !agency && mode == "create" {
				sets = append(append([][]importField{}, sets...), importEntityCreate...)
				sets = append(sets, importRevenue...)
			}
			var fields, salaries bool
			fields, err = applyImportFields(entity, row, sets...)
			if err == nil && !agency && mode == "create" {
				if score := importText(row, "score"); score != "" {
					if _, numberErr := importNumber(score, 0, 100, true); numberErr != nil {
						err = fmt.Errorf("Итоговая оценка: %w", numberErr)
					}
				}
			}
			if err == nil {
				salaries, err = applyImportSalaries(entity, row, agency)
			}
			if err == nil && !agency {
				if score, _ := entity["aiScore"].(float64); score > 0 && asText(entity["aiEvidence"]) == "" {
					err = fmt.Errorf("для балла ИИ нужно обоснование")
				}
			}
			if err == nil && mode == "update" && !fields && !salaries {
				err = fmt.Errorf("не заполнено ни одно поле для обновления")
			}
			if err == nil {
				if mode == "create" {
					index = len(o.entities)
					o.entities = append(o.entities, entity)
					o.names[importKey(name)] = index
					if id > 0 {
						o.sourceIDs[id] = index
					}
					indexSources(entity)
					report.counts[added]++
				} else {
					o.entities[index] = entity
					used[index] = true
					report.counts[updated]++
				}
				continue
			}
		}
		report.counts[skipped]++
		report.issue(sheet, row, i, err)
	}
}

func (o *importOwners) importOffices(rows []ImportRow, mode string, report *importReport) {
	touched := map[[2]int]bool{}
	for i, row := range rows {
		id, err := importID(row, "companyId")
		name, address := importText(row, "companyName"), importText(row, "address")
		index, found, lookupErr := o.lookup(id, name, mode == "create")
		if err == nil {
			err = lookupErr
		}
		lat, lon := importText(row, "lat"), importText(row, "lon")
		if err == nil && mode == "update" && lat == "" && lon == "" {
			report.counts["officesSkipped"]++
			continue
		}
		if err == nil && (!found || address == "" || len(address) > 4000) {
			err = fmt.Errorf("не найдена компания или не указан адрес")
		}
		if err == nil && mode == "create" && index < len(o.entities)-report.counts["added"] {
			err = fmt.Errorf("компания не добавлена из этого файла")
		}
		var latitude, longitude any
		if err == nil && (lat != "" || lon != "") {
			latitude, err = importNumber(lat, -90, 90, false)
			if err == nil {
				longitude, err = importNumber(lon, -180, 180, false)
			}
			if err != nil {
				err = fmt.Errorf("укажите корректные широту и долготу вместе")
			}
		}
		if err == nil {
			entity := o.entities[index]
			offices := asSlice(entity["offices"])
			if mode == "create" {
				office := storedEntity{"address": address, "lat": latitude, "lon": longitude}
				_, err = applyImportFields(office, row, importFields("role metro status main", "text"), importFields("source", "url"))
				if main := importText(row, "main"); main != "" && main != "Да" && main != "Нет" {
					err = fmt.Errorf("основной офис: укажите «Да» или «Нет»")
				}
				if importText(row, "main") == "Да" {
					for _, v := range offices {
						if asText(obj(v)["main"]) == "Да" {
							err = fmt.Errorf("основной офис уже указан")
						}
					}
				}
				if err == nil {
					entity["offices"] = append(offices, map[string]any(office))
					report.counts["officesAdded"]++
					continue
				}
			} else {
				position := -1
				for j, v := range offices {
					if importKey(asText(obj(v)["address"])) == importKey(address) {
						position = j
						break
					}
				}
				key := [2]int{index, position}
				if position < 0 || touched[key] {
					err = fmt.Errorf("офис не найден или уже обновлён")
				} else {
					office := obj(offices[position])
					office["lat"], office["lon"] = latitude, longitude
					touched[key] = true
					report.counts["officesUpdated"]++
					continue
				}
			}
		}
		report.counts["officesSkipped"]++
		if mode == "update" {
			report.counts["invalid"]++
		}
		report.issue("Офисы", row, i, err)
	}
}

func (o *importOwners) importReviewRows(rows []ImportRow, mode string, report *importReport) {
	touched := map[int]bool{}
	for i, row := range rows {
		id, err := importID(row, "companyId")
		index, found, _ := o.lookup(id, "", mode == "create")
		if err == nil && (!found || touched[index]) {
			err = fmt.Errorf("компания по ID не найдена или уже обновлена на листе «Отзывы»")
		}
		if err == nil {
			entity := maps.Clone(o.entities[index])
			var changed bool
			changed, err = applyImportFields(entity, row, importReviews)
			if err == nil {
				if changed {
					o.entities[index] = entity
					touched[index] = true
					report.counts["reviewsUpdated"]++
				} else {
					report.counts["reviewsSkipped"]++
				}
				continue
			}
		}
		report.counts["invalid"]++
		report.counts["reviewsSkipped"]++
		report.issue("Отзывы", row, i, err)
	}
}

func importIdentityIDs(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]int64{}
	for rows.Next() {
		var name string
		var id int64
		if err = rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		ids[name] = id
	}
	return ids, rows.Err()
}

func (r Repository) ImportCompanies(ctx context.Context, input CompanyImport) (CompanyImportResult, error) {
	if input.Mode == "" {
		input.Mode = "create"
	}
	if input.Mode != "create" && input.Mode != "update" {
		return nil, ImportError("Неизвестный режим импорта.")
	}
	if len(input.Companies) == 0 {
		return nil, ImportError("На листе «Компании» нет строк для импорта.")
	}
	if len(input.Companies) > 500 || len(input.Offices) > 2000 || len(input.Reviews) > 500 || len(input.Agencies) > 500 || len(input.Contacts) > 1500 {
		return nil, ImportError("Превышено допустимое количество строк одного из листов.")
	}
	if r.DB == nil {
		return nil, errors.New("catalog import storage unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return nil, err
	}
	var data []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1 FOR UPDATE").Scan(&data); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	normalized, err := backup.NormalizeStoredCatalog(data)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(normalized, &root); err != nil {
		return nil, err
	}
	var source sourceCatalog
	if err = json.Unmarshal(normalized, &source); err != nil {
		return nil, err
	}
	companyIDs, err := importIdentityIDs(ctx, tx, "SELECT company_name,id FROM company_identity")
	if err != nil {
		return nil, err
	}
	agencyIDs, err := importIdentityIDs(ctx, tx, "SELECT agency_name,id FROM agency_identity")
	if err != nil {
		return nil, err
	}
	companies, agencies := newImportOwners(source.Companies, companyIDs), newImportOwners(source.Agencies, agencyIDs)
	report := importReport{counts: map[string]int{}, issues: []CompanyImportIssue{}, duplicates: []CompanyImportDuplicate{}}
	companies.importEntities(input.Companies, "Компании", "companyId", input.Mode, false, &report)
	companies.importOffices(input.Offices, input.Mode, &report)
	companies.importReviewRows(input.Reviews, input.Mode, &report)
	agencies.importEntities(input.Agencies, "Агентства", "agencyId", input.Mode, true, &report)
	for _, kind := range []struct {
		owners *importOwners
		count  string
		query  string
	}{
		{companies, "added", "INSERT INTO company_identity(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)"},
		{agencies, "agenciesAdded", "INSERT INTO agency_identity(agency_name) VALUES(?) ON DUPLICATE KEY UPDATE agency_name=VALUES(agency_name)"},
	} {
		for _, entity := range kind.owners.entities[len(kind.owners.entities)-report.counts[kind.count]:] {
			name := nameOf(entity)
			if _, err = tx.ExecContext(ctx, kind.query, name); err != nil {
				return nil, err
			}
			if err = ensureImportCRM(ctx, tx, name); err != nil {
				return nil, err
			}
		}
	}
	changed := report.counts["added"]+report.counts["updated"]+report.counts["officesAdded"]+report.counts["officesUpdated"]+report.counts["reviewsUpdated"]+report.counts["agenciesAdded"]+report.counts["agenciesUpdated"] > 0
	if changed {
		for field, value := range map[string]any{"companies": companies.entities, "agencies": agencies.entities, "updated": time.Now().UTC().Format("2006-01-02")} {
			root[field], err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
		}
		data, err = json.Marshal(root)
		if err != nil {
			return nil, err
		}
		data, err = backup.NormalizeStoredCatalog(data)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_catalog(id,data_json,updated_at) VALUES(1,?,CURRENT_TIMESTAMP)
			ON DUPLICATE KEY UPDATE data_json=VALUES(data_json),updated_at=CURRENT_TIMESTAMP`, data); err != nil {
			return nil, err
		}
	}
	if err = importContactRows(ctx, tx, input.Contacts, input.Mode, companies, agencies, &report); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return report.result(), nil
}

func ensureImportCRM(ctx context.Context, tx *sql.Tx, name string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)", name)
	return err
}
