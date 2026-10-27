package backup

import "itmap/internal/v39"

// Introductions and removals match unchanged validateFullBackup. These describe
// uploaded layouts only; destination schema always comes from trusted v39.
var tableSince = map[string]int{
	"user_profiles": 18, "openrouter_settings": 19, "openrouter_ai_logs": 19, "system_prompts": 20, "vacancy_cover_letters": 21, "openrouter_model_metrics": 22, "prompt_model_settings": 23, "openai_settings": 24,
	"vacancy_roles": 27, "vacancy_role_assignments": 27, "vacancy_role_classification": 28, "vacancy_candidates": 29, "vacancy_candidate_identities": 29, "vacancy_classification_attempts": 29, "vacancy_classifications": 29,
	"firecrawl_settings": 30, "vacancy_source_parsing": 30, "vacancy_detail_settings": 32, "vacancy_raw_snapshots": 32, "vacancy_raw_snapshot_parts": 32, "vacancy_content_fetches": 32, "vacancy_extractions": 32,
	"vacancy_role_settings": 33, "vacancy_role_results": 33, "vacancy_parser_fields": 33, "vacancy_parser_roles": 33, "vacancy_publication_sources": 33, "vacancy_publication_keys": 33, "vacancy_import_operations": 33,
	"apify_settings": 36, "parser_error_daily": 39, "parser_candidate_intakes": 39,
}

func historicalDefinition(t v39.Table, version int, incoming Table) v39.Table {
	present := map[string]bool{}
	for _, raw := range incoming.Columns {
		v, e := orderedJSON(raw)
		if e == nil {
			present[textField(v, "name")] = true
		}
	}
	out := t
	out.Columns = nil
	for _, c := range t.Columns {
		omit := false
		switch t.Name {
		case "vacancy_cover_letters":
			omit = version < 25 && !present["override_shared"] && !present["revision"] && (c.Name == "override_shared" || c.Name == "revision")
		case "prompt_model_settings":
			omit = version < 26 && !present["openrouter_model"] && c.Name == "openrouter_model"
		case "vacancy_role_settings":
			omit = version < 37 && c.Name == "openrouter_model"
		case "vacancy_candidates", "vacancy_source_parsing":
			omit = version < 38 && c.Name == "agency_id"
			if t.Name == "vacancy_source_parsing" && version < 35 {
				switch c.Name {
				case "auto_enabled", "interval_days", "next_run_at", "last_attempt_at", "last_result":
					omit = true
				}
			}
		case "firecrawl_settings":
			if version < 31 {
				switch c.Name {
				case "mode", "hobby_key_encrypted", "hobby_key_suffix", "hobby_connection_status", "hobby_last_error", "hobby_checked_at", "free_key_hash", "hobby_key_hash", "free_usage_json", "hobby_usage_json":
					omit = true
				}
			}
			omit = omit || version < 34 && c.Name == "favorites_only" || version < 35 && c.Name == "scheduler_json"
		}
		if !omit {
			out.Columns = append(out.Columns, c)
		}
	}
	return out
}
