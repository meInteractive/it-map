package backup

import (
	"encoding/json"
	"errors"
	"math"
	"unicode/utf16"
)

var ErrSchedulerAttemptProjection = errors.New("invalid selected scheduler attempt projection")

// PrepareSchedulerAttemptJSON projects one explicit schedulerDashboard SELECT
// row, not arbitrary provider data. Unknown columns fail closed; result_json is
// always removed. SQL selection, 2048-character truncation and transactions are
// the caller's responsibility. This performs no SQL or external work.
func PrepareSchedulerAttemptJSON(data []byte) (json.RawMessage, error) {
	row, e := orderedJSON(data)
	if e != nil || row.kind != '{' {
		return nil, ErrSchedulerAttemptProjection
	}
	for _, member := range row.members {
		switch string(utf16.Decode(member.key)) {
		case "id", "job_id", "run_id", "type", "company_id", "agency_id", "source_id", "candidate_id", "started_at", "finished_at", "status", "reason_code", "message", "provider", "model", "reserved_usd", "cost_usd", "credits", "result_json":
		default:
			return nil, ErrSchedulerAttemptProjection
		}
	}
	typeValue := row.field("type")
	if typeValue == nil || typeValue.kind != 's' {
		return nil, ErrSchedulerAttemptProjection
	}
	out := catalogObject()
	catalogSpread(out, row)
	catalogDelete(out, "result_json")
	resultText := row.field("result_json")
	if resultText == nil || resultText.kind == 'n' {
		return rawValue(catalogOrderedValue(out)), nil
	}
	if resultText.kind != 's' {
		return nil, ErrSchedulerAttemptProjection
	}
	result, e := orderedJSON([]byte(string(utf16.Decode(resultText.text))))
	if e != nil {
		// Source catches JSON.parse failures, including a truncated SQL substr.
		return rawValue(catalogOrderedValue(out)), nil
	}
	switch string(utf16.Decode(typeValue.text)) {
	case "classify_title":
		accepted := result.field("accepted")
		if accepted != nil && accepted.kind == 'b' {
			outcome := catalogObject()
			outcome.set("accepted", accepted)
			out.set("outcome", outcome)
		}
	case "assign_roles":
		ids := result.field("roleIds")
		if ids != nil && ids.kind == '[' && len(ids.items) <= 12 {
			seen, valid := map[float64]bool{}, true
			for _, id := range ids.items {
				if id.kind != 'f' || id.number <= 0 || id.number > maxSafeInteger || math.Trunc(id.number) != id.number || seen[id.number] {
					valid = false
					break
				}
				seen[id.number] = true
			}
			if valid {
				outcome := catalogObject()
				outcome.set("roleIds", ids)
				outcome.set("filtered", readBool(len(ids.items) == 0))
				out.set("outcome", outcome)
			}
		}
	case "import_vacancy":
		superseded, next := result.field("superseded"), result.field("nextStage")
		allowed := next != nil && next.kind == 'n'
		if next != nil && next.kind == 's' {
			switch string(utf16.Decode(next.text)) {
			case "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance":
				allowed = true
			}
		}
		if superseded != nil && superseded.kind == 'b' && superseded.boolean && allowed {
			outcome := catalogObject()
			outcome.set("superseded", readBool(true))
			outcome.set("nextStage", next)
			out.set("outcome", outcome)
		}
	}
	return rawValue(catalogOrderedValue(out)), nil
}
