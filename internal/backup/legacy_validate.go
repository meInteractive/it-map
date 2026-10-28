package backup

import (
	"encoding/json"
	"math"

	"itmap/internal/v39"
)

// ValidateLegacyBackup validates unsigned full v14–15 files without SQL or
// normalization. Their metadata, integrity and counters were not guaranteed by
// the source format and are deliberately not promoted into trusted plan fields.
// This does not enable runtime restore; modern and v39 entry points stay strict.
func ValidateLegacyBackup(data []byte) (*Payload, error) {
	if len(data) > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Файл бэкапа больше допустимых 25 МБ."}
	}
	root, e := orderedJSON(data)
	if e != nil {
		return nil, e
	}
	version := root.field("version")
	if textField(root, "format") != Format || version == nil || version.kind != 'f' || version.number != 14 && version.number != 15 {
		return nil, invalid("Поддерживаются legacy-бэкапы v14–15.")
	}
	if e = validateLegacyCatalog(root.field("catalog"), int(version.number)); e != nil {
		return nil, e
	}
	nodes := root.field("storage").field("tables")
	if nodes == nil || nodes.kind != '[' || len(nodes.items) > MaxTables {
		return nil, invalid("Некорректный состав данных бэкапа.")
	}
	// Match the legacy preliminary structural pass before required columns and
	// full row/layout checks. Keep the ordered tree until those passes finish.
	byNode := map[string]*jsonValue{}
	total := 0
	for _, node := range nodes.items {
		name, cols, rows := node.field("name"), node.field("columns"), node.field("rows")
		if name == nil || name.kind != 's' || cols == nil || cols.kind != '[' || rows == nil || rows.kind != '[' {
			return nil, invalid("Некорректный раздел данных в бэкапе.")
		}
		key := stringFromUnits(name.text)
		if _, exists := byNode[key]; exists {
			return nil, invalid("В бэкапе повторяется раздел данных.")
		}
		byNode[key] = node
		total += len(rows.items)
		if total > MaxRows {
			return nil, invalid("В бэкапе слишком много записей.")
		}
	}
	if e = legacyRequiredColumns(byNode); e != nil {
		return nil, e
	}
	definitions := map[string]v39.Table{}
	for _, d := range v39.Trusted().Tables {
		if tableSince[d.Name] == 0 && d.Name != "app_catalog" && d.Name != "google_calendar_connection" {
			definitions[d.Name] = d
			if d.Name != "vacancy_audit_runs" && byNode[d.Name] == nil {
				return nil, invalid("Старый бэкап неполон: отсутствуют обязательные таблицы. Восстановление отменено.")
			}
		}
	}
	for name := range byNode {
		if _, exists := definitions[name]; !exists {
			return nil, invalid("Старый бэкап неполон: отсутствуют обязательные таблицы. Восстановление отменено.")
		}
	}
	p := &Payload{legacyCatalog: root.field("catalog"), legacyCreatedAt: root.field("createdAt"), Format: Format, Version: version.number, CreatedAt: rawValue(root.field("createdAt")), Catalog: rawValue(root.field("catalog"))}
	p.Storage.Tables = make([]Table, 0, len(nodes.items))
	byTable := map[string]Table{}
	for _, node := range nodes.items {
		name := textField(node, "name")
		t := Table{Name: name, Columns: []json.RawMessage{}, Rows: []map[string]json.RawMessage{}}
		for _, c := range node.field("columns").items {
			t.Columns = append(t.Columns, rawValue(c))
		}
		names, e := checkColumns(t, definitions[name])
		if e != nil {
			return nil, invalid("Неполная или несовместимая структура раздела \"" + name + "\". Обновите приложение и создайте полный бэкап.")
		}
		for _, row := range node.field("rows").items {
			if row.kind != '{' || len(row.members) != len(names) {
				return nil, invalid("Неполная или некорректная запись в разделе \"" + name + "\".")
			}
			r := map[string]json.RawMessage{}
			for _, col := range names {
				v := row.field(col)
				if v == nil || v.kind != 'n' && v.kind != 's' && (v.kind != 'f' || math.IsInf(v.number, 0) || math.IsNaN(v.number)) {
					return nil, invalid("Неполная или некорректная запись в разделе \"" + name + "\".")
				}
				r[col] = rawValue(v)
			}
			t.Rows = append(t.Rows, r)
		}
		p.Storage.Tables = append(p.Storage.Tables, t)
		byTable[name] = t
	}
	for _, d := range v39.Trusted().Tables {
		for _, fk := range d.ForeignKeys {
			parents := map[string]bool{}
			for _, row := range byTable[fk.Table].Rows {
				parents[string(row[fk.To])] = true
			}
			for _, row := range byTable[d.Name].Rows {
				value := string(row[fk.From])
				if value != "null" && !parents[value] {
					return nil, invalid("Нарушена связь раздела \"" + d.Name + "\" с \"" + fk.Table + "\".")
				}
			}
		}
	}
	return p, nil
}

func legacyRequiredColumns(tables map[string]*jsonValue) error {
	has := func(name string, columns []string) bool {
		t := tables[name]
		if t == nil {
			return false
		}
		found := map[string]bool{}
		for _, c := range t.field("columns").items {
			if v := c.field("name"); jsonTruthy(v) {
				found[stringFromUnits(legacyString(v))] = true
			}
		}
		for _, c := range columns {
			if !found[c] {
				return false
			}
		}
		return true
	}
	// Preserve source check order and diagnostics; these precede full layouts.
	for _, spec := range []struct {
		name string
		cols []string
	}{
		{"company_tasks", []string{"id", "company_name", "vacancy_id", "type", "note", "due_at", "completed", "created_at", "completed_at"}},
		{"company_vacancies", []string{"id", "company_name", "url", "title", "role", "level", "relevance", "loaded_at", "description_md", "application_status"}},
		{"company_applications", []string{"id", "company_name", "vacancy_id", "status", "comment_md", "recruiter_name", "recruiter_phone", "recruiter_email", "recruiter_contact_type"}},
		{"company_application_status_history", []string{"id", "application_id", "from_status", "to_status", "changed_at"}},
	} {
		if !has(spec.name, spec.cols) {
			return invalid("В бэкапе версии 2 отсутствует актуальная структура раздела \"" + spec.name + "\".")
		}
	}
	if !has("company_applications", []string{"application_sources"}) {
		return invalid("В бэкапе отсутствуют источники откликов.")
	}
	for _, spec := range []struct {
		name string
		cols []string
	}{
		{"company_tasks", []string{"user_name"}}, {"company_applications", []string{"user_name"}}, {"company_status_history", []string{"user_name"}},
		{"company_user_state", []string{"user_name", "company_name"}}, {"company_vacancy_user_state", []string{"user_name", "vacancy_id"}},
	} {
		if !has(spec.name, spec.cols) {
			return invalid("В бэкапе отсутствуют пользовательские данные раздела \"" + spec.name + "\".")
		}
	}
	if !has("company_user_state", []string{"hidden"}) {
		return invalid("В бэкапе отсутствует признак скрытых компаний.")
	}
	for _, spec := range []struct {
		name, message string
		cols          []string
	}{
		{"vacancy_employer_guesses", "В бэкапе отсутствуют предположения о реальных работодателях.", []string{"agency_vacancy_id", "position", "employer_name", "linked_company_name", "matched_vacancy_id", "external_vacancy_url"}},
		{"company_vacancies", "В бэкапе отсутствуют сопроводительные письма вакансий.", []string{"cover_letter_subject", "cover_letter_md"}},
		{"hiring_contacts", "В бэкапе отсутствуют контакты ответственных за найм.", []string{"company_name", "position", "full_name", "linkedin_url", "facebook_url", "telegram", "phone"}},
		{"hiring_contacts", "В бэкапе отсутствуют email и специализация контактов.", []string{"email", "specialization"}},
		{"hiring_contacts", "В бэкапе отсутствуют ссылки на профили «Сетка».", []string{"setka_url"}},
		{"telegram_channels", "В бэкапе отсутствуют независимые Telegram-каналы.", []string{"name", "url", "normalized_username", "username", "last_successful_check_at", "comment"}},
		{"vacancy_telegram_sources", "В бэкапе отсутствуют связи Telegram-каналов с вакансиями.", []string{"vacancy_id", "channel_id", "message_url", "telegram_contact"}},
	} {
		if !has(spec.name, spec.cols) {
			return invalid(spec.message)
		}
	}
	return nil
}
