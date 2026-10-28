package backup

import (
	"encoding/json"
	"time"
)

// PrepareRestore is the pure planner for the full source v14–39 input boundary.
// Modern-specific entry points remain signed and never accept legacy files.
func PrepareRestore(data []byte, current []Table, now time.Time) (*RestorePlan, error) {
	payload, e := validateRestoreInput(data)
	if e != nil {
		return nil, e
	}
	return prepareHistoricalPayload(payload, current, now)
}
func validateRestoreInput(data []byte) (*Payload, error) {
	// Bound input before the dispatch pass copies RawMessage values. A map keeps
	// property names case-sensitive and follows the source's last duplicate wins.
	if len(data) > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Файл бэкапа больше допустимых 25 МБ."}
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) == nil {
		var version float64
		if json.Unmarshal(root["version"], &version) == nil && (version == 14 || version == 15) {
			return ValidateLegacyBackup(data)
		}
	}
	return ValidateModernBackup(data)
}
func preservedRestoreTables(payload *Payload) map[string]bool {
	names := preservedTables(int(payload.Version))
	if payload.Version < 16 {
		names["google_calendar_connection"] = true
		names["vacancy_audit_runs"] = true
		for _, table := range payload.Storage.Tables {
			if table.Name == "vacancy_audit_runs" {
				delete(names, "vacancy_audit_runs")
				break
			}
		}
	}
	return names
}
