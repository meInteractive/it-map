package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"itmap/internal/integrations/apify"
)

type hhCandidateKey struct{ company, candidate int64 }

// hhVerifiedLineage принимает только кандидатов из проверенного HH-листинга.
// Сохранённые до появления фильтров списки не становятся подтверждёнными.
func hhVerifiedLineage(raw []byte, generation string, company int64, source string, revision int64) []int64 {
	ids, _ := hhCursorProof(raw, generation, company, source, revision)
	return ids
}

func hhCursorProof(raw []byte, generation string, company int64, source string, revision int64) ([]int64, bool) {
	var c hhListingCursor
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.LineageVersion != 1 || !c.validLineage() || !c.MoscowFilter || !c.DateFilterApplied || c.TitleQuery != nil || len(c.ProfessionalRoles) == 0 || c.Operation.Generation != generation || c.Operation.CompanyID != company || c.Operation.SourceURL != source || c.Operation.SourceRevision != revision {
		return nil, false
	}
	if c.VacancyAgeDays != 0 && c.VacancyAgeDays != 3 && c.VacancyAgeDays != 7 && c.VacancyAgeDays != 14 {
		return nil, false
	}
	if c.VacancyAgeDays == 0 && (c.DateFrom != nil || c.DateTo != nil) || c.VacancyAgeDays != 0 && (c.DateFrom == nil || c.DateTo == nil) {
		return nil, false
	}
	if _, err := apify.HHInput(source, c.options()); err != nil {
		return nil, false
	}
	return c.CandidateIDs, true
}

// readVerifiedHHLineage оставляет контракт планировщика прежним.
// Причины отказа доступны только диагностике; выбор задания их не использует.
func readVerifiedHHLineage(ctx context.Context, tx *sql.Tx, generation string) (map[hhCandidateKey]bool, map[string]bool, error) {
	verified, blocked, _, err := readVerifiedHHLineageReasons(ctx, tx, generation)
	return verified, blocked, err
}

const (
	hhBlockedSourceChanged = "source_changed"
	hhBlockedLegacyFilters = "legacy_filters"
	hhBlockedMalformedCursor = "malformed_cursor"
	hhBlockedOtherProof = "other_proof"
)

// Причины классифицируются строго на уже заблокированных курсорах.
// Сначала проверяется текущая версия источника, затем формат курсора.
func hhBlockedLineageReason(raw []byte, current bool) string {
	if !current {
		return hhBlockedSourceChanged
	}
	var cursor hhListingCursor
	if json.Unmarshal(raw, &cursor) != nil {
		return hhBlockedMalformedCursor
	}
	if cursor.Version != 1 || cursor.LineageVersion != 1 || !cursor.MoscowFilter ||
		!cursor.DateFilterApplied || cursor.TitleQuery != nil || len(cursor.ProfessionalRoles) == 0 {
		return hhBlockedLegacyFilters
	}
	return hhBlockedOtherProof
}

// readVerifiedHHLineageReasons читает те же записи и использует тот же
// proof, что планировщик, но отдельно помечает причины отказа для GET-only.
func readVerifiedHHLineageReasons(ctx context.Context, tx *sql.Tx, generation string) (map[hhCandidateKey]bool, map[string]bool, map[string]string, error) {
	out := map[hhCandidateKey]bool{}
	blockedScans := map[string]bool{}
	blockedReasons := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.company_id,d.source_url,d.source_revision,d.result_json,COALESCE(s.enabled=1 AND s.revision=d.source_revision AND s.source_url=d.source_url,0) FROM vacancy_discovery_runs d LEFT JOIN vacancy_source_parsing s ON s.company_id=d.company_id AND s.source_id='hh' WHERE d.generation=? AND d.source_id='hh' AND d.company_id IS NOT NULL AND d.agency_id IS NULL`, generation)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var company, revision int64
		var id, source string
		var raw []byte
		var current bool
		if err = rows.Scan(&id, &company, &source, &revision, &raw, &current); err != nil {
			return nil, nil, nil, err
		}
		candidates, valid := hhCursorProof(raw, generation, company, source, revision)
		if !current || !valid {
			if strings.HasPrefix(id, "go399:") {
				scanID := strings.TrimPrefix(id, "go399:")
				blockedScans[scanID] = true
				blockedReasons[scanID] = hhBlockedLineageReason(raw, current)
			}
			continue
		}
		for _, candidate := range candidates {
			out[hhCandidateKey{company, candidate}] = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return out, blockedScans, blockedReasons, nil
}
