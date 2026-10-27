package backup

import (
	"encoding/json"
	"itmap/internal/v39"
	"time"
)

// PrepareModernRestore is a pure transformation of signed v16–39 input plus a
// caller-owned destination snapshot. SQL callers must capture preserved rows
// under their data transaction locks, never reuse a stale validation snapshot.
// The clock is injected because old single-role migration may need timestamps.
func PrepareModernRestore(data []byte, current []Table, now time.Time) (*RestorePlan, error) {
	payload, e := ValidateModernBackup(data)
	if e != nil {
		return nil, e
	}
	return prepareModernPayload(payload, current, now)
}
func prepareModernPayload(payload *Payload, current []Table, now time.Time) (*RestorePlan, error) {
	version := int(payload.Version)
	tables := payload.Storage.Tables
	// Payload owns its rows, while destination state is copied before transforms.
	defs := map[string]v39.Table{}
	for _, d := range v39.Trusted().Tables {
		defs[d.Name] = d
	}
	existing := map[string]Table{}
	for _, t := range current {
		t.Rows = cloneRows(t.Rows)
		existing[t.Name] = t
	}
	fullColumns := func(name string) []json.RawMessage {
		out := []json.RawMessage{}
		for _, c := range defs[name].Columns {
			raw, _ := json.Marshal(c)
			out = append(out, raw)
		}
		return out
	}
	get := func(name string) *Table {
		for i := range tables {
			if tables[i].Name == name {
				return &tables[i]
			}
		}
		return nil
	}
	appendRows := func(name string, rows []map[string]json.RawMessage) {
		tables = append(tables, Table{Name: name, Columns: fullColumns(name), Rows: rows})
	}
	preserve := func(name string) {
		rows := existing[name].Rows
		if rows == nil {
			rows = []map[string]json.RawMessage{}
		}
		appendRows(name, rows)
	}
	empty := func(name string) { appendRows(name, []map[string]json.RawMessage{}) }
	for i := range tables {
		t := &tables[i]
		switch {
		case version < 25 && t.Name == "vacancy_cover_letters":
			t.Columns = fullColumns(t.Name)
			for _, r := range t.Rows {
				for _, k := range []string{"override_shared", "revision"} {
					v, e := orderedJSON(r[k])
					if e != nil || v.kind == 'n' {
						r[k] = json.RawMessage("0")
					}
				}
			}
		case version < 37 && t.Name == "vacancy_role_settings":
			t.Columns = fullColumns(t.Name)
			for _, r := range t.Rows {
				r["openrouter_model"] = rawString("best_free")
			}
		case version < 26 && t.Name == "prompt_model_settings":
			t.Columns = fullColumns(t.Name)
			for _, r := range t.Rows {
				v, e := orderedJSON(r["openrouter_model"])
				if e != nil || v.kind == 'n' {
					r["openrouter_model"] = rawString("best_free")
				}
			}
		}
	}
	for _, spec := range []struct {
		before int
		names  []string
	}{{18, []string{"user_profiles"}}, {19, []string{"openrouter_settings", "openrouter_ai_logs"}}, {20, []string{"system_prompts"}}, {21, []string{"vacancy_cover_letters"}}, {22, []string{"openrouter_model_metrics"}}, {23, []string{"prompt_model_settings"}}, {24, []string{"openai_settings"}}} {
		if version < spec.before {
			for _, n := range spec.names {
				preserve(n)
			}
		}
	}
	if version < 27 {
		date := payload.CreatedAt
		v, _ := orderedJSON(date)
		if !jsonTruthy(v) {
			date = rawString(now.UTC().Format("2006-01-02T15:04:05.000Z"))
		}
		roles, assignments := migratedLegacyRoles(get("company_vacancies").Rows, date)
		appendRows("vacancy_roles", roles)
		appendRows("vacancy_role_assignments", assignments)
	}
	if version < 28 {
		byKey := map[string]json.RawMessage{}
		for _, r := range get("vacancy_roles").Rows {
			byKey[string(r["stable_key"])] = r["id"]
		}
		prior := map[string]string{}
		for _, r := range existing["vacancy_roles"].Rows {
			prior[string(r["id"])] = string(r["stable_key"])
		}
		saved := []map[string]json.RawMessage{}
		for _, r := range existing["vacancy_role_classification"].Rows {
			if id, ok := byKey[prior[string(r["role_id"])]]; ok {
				out := cloneRow(r)
				out["role_id"] = id
				saved = append(saved, out)
			}
		}
		appendRows("vacancy_role_classification", saved)
	}
	if version < 29 {
		for _, n := range []string{"vacancy_candidates", "vacancy_candidate_identities", "vacancy_classification_attempts", "vacancy_classifications"} {
			empty(n)
		}
	}
	if version < 30 {
		preserve("firecrawl_settings")
		empty("vacancy_source_parsing")
	}
	if version < 38 {
		for _, n := range []string{"vacancy_candidates", "vacancy_source_parsing"} {
			t := get(n)
			t.Columns = fullColumns(n)
			for _, r := range t.Rows {
				r["agency_id"] = json.RawMessage("null")
			}
		}
	}
	settings := get("firecrawl_settings")
	settings.Columns = fullColumns(settings.Name)
	if version < 31 {
		for _, r := range settings.Rows {
			for k, v := range map[string]string{"mode": "free", "hobby_connection_status": "unchecked", "hobby_last_error": "", "free_key_hash": "", "hobby_key_hash": "", "free_usage_json": "", "hobby_usage_json": "", "proxy": "basic"} {
				r[k] = rawString(v)
			}
			for _, k := range []string{"hobby_key_encrypted", "hobby_key_suffix"} {
				v, _ := orderedJSON(r[k])
				if !jsonTruthy(v) {
					r[k] = rawString("")
				}
			}
			r["hobby_checked_at"] = json.RawMessage("null")
			r["enhanced_allowed"] = json.RawMessage("0")
			v, _ := orderedJSON(r["revision"])
			if !jsonTruthy(v) {
				r["revision"] = json.RawMessage("1")
			} else if v.kind == 's' {
				r["revision"] = rawText(append(append([]uint16(nil), v.text...), '1'))
			} else {
				v.number++
				r["revision"] = rawValue(v)
			}
		}
	}
	if version < 34 {
		for _, r := range settings.Rows {
			r["favorites_only"] = json.RawMessage("1")
		}
	}
	if version < 35 {
		for _, r := range settings.Rows {
			r["scheduler_json"] = rawString("{}")
		}
	}
	// Scheduler/Apify safety is shared with current v39 and applied after all
	// destination omissions have been filled. Old source rows have no auto flags.
	if version < 35 {
		t := get("vacancy_source_parsing")
		t.Columns = fullColumns(t.Name)
		for _, r := range t.Rows {
			r["auto_enabled"] = json.RawMessage("0")
			r["interval_days"] = json.RawMessage("7")
			r["next_run_at"] = json.RawMessage("null")
			r["last_attempt_at"] = json.RawMessage("null")
			r["last_result"] = rawString("")
		}
	}
	if version < 36 {
		preserve("apify_settings")
	}
	if version < 33 {
		for _, n := range []string{"vacancy_role_settings", "vacancy_role_results", "vacancy_parser_fields", "vacancy_parser_roles", "vacancy_publication_sources", "vacancy_publication_keys", "vacancy_import_operations"} {
			empty(n)
		}
	}
	if version < 32 {
		for _, n := range []string{"vacancy_detail_settings", "vacancy_raw_snapshots", "vacancy_raw_snapshot_parts", "vacancy_content_fetches", "vacancy_extractions"} {
			empty(n)
		}
	}
	if version < 39 {
		preserve("parser_error_daily")
		tokens := map[string]bool{}
		for _, r := range get("vacancy_candidates").Rows {
			tokens[string(r["creation_token"])] = true
		}
		rows := []map[string]json.RawMessage{}
		for _, r := range existing["parser_candidate_intakes"].Rows {
			if tokens[string(r["candidate_token"])] {
				rows = append(rows, cloneRow(r))
			}
		}
		appendRows("parser_candidate_intakes", rows)
	}
	payload.Storage.Tables = tables
	plan, e := finishRestorePlan(payload)
	if e != nil {
		return nil, e
	}
	plan.Warnings = append([]string(nil), historicalPolicy().Warnings[version]...)
	return plan, nil
}
