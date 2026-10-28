package backup

import (
	"encoding/json"
	"sort"
	"time"

	"itmap/internal/rucollation"
)

// PrepareLegacyRestore builds a pure, owned v14–15 plan. Untrusted metadata is
// never used as SQL or as a counter source. SQL/HTTP entry points rebuild the
// plan under destination/generation locks rather than reusing a read-only plan.
func PrepareLegacyRestore(data []byte, current []Table, now time.Time) (*RestorePlan, error) {
	payload, e := ValidateLegacyBackup(data)
	if e != nil {
		return nil, e
	}
	return prepareHistoricalPayload(payload, current, now)
}

// normalizeLegacyCatalog is called only after legacy validation. The original
// catalog is kept for source response counts; only the stored app_catalog JSON
// is normalized. Names/IDs/FKs are never replaced by comparison keys.
func normalizeLegacyCatalog(data json.RawMessage) (json.RawMessage, error) {
	source, e := orderedJSON(data)
	if e != nil {
		return nil, e
	}
	return normalizeLegacyCatalogValue(source)
}

func normalizeLegacyCatalogValue(source *jsonValue) (json.RawMessage, error) {
	type item struct {
		entity *jsonValue
		key    rucollation.Key
	}
	companies := make([]item, 0, len(source.field("companies").items))
	for _, c := range source.field("companies").items {
		entity, e := normalizeLegacyCompanyFields(c, 0)
		if e != nil {
			return nil, e
		}
		companies = append(companies, item{entity, rucollation.Keys(entity.field("name").text)})
	}
	sort.SliceStable(companies, func(i, j int) bool {
		a, b := companies[i], companies[j]
		delta := b.entity.field("score").number - a.entity.field("score").number
		if delta != 0 {
			return delta < 0
		}
		return rucollation.Compare(a.key, b.key) < 0
	})
	out := catalogObject()
	catalogSpread(out, source)
	normalized := &jsonValue{kind: '[', items: make([]*jsonValue, 0, len(companies))}
	for i, c := range companies {
		c.entity.set("rank", catalogNumber(float64(i+1)))
		normalized.items = append(normalized.items, c.entity)
	}
	out.set("companies", normalized)
	agencies := &jsonValue{kind: '[', items: []*jsonValue{}}
	if list := source.field("agencies"); list != nil && list.kind == '[' {
		for _, agency := range list.items {
			name := agency.field("name")
			if name == nil || name.kind != 's' || len(trimUnits(name.text)) == 0 {
				continue
			}
			entity, e := normalizeLegacyAgencyFields(agency)
			if e != nil {
				return nil, e
			}
			agencies.items = append(agencies.items, entity)
		}
	}
	out.set("agencies", agencies)
	return rawValue(out), nil
}
