package backup

import (
	"context"
	"encoding/json"
	"errors"
)

type RestoreValidation struct {
	Tables   int      `json:"tables"`
	Rows     int      `json:"rows"`
	Warnings []string `json:"warnings"`
}

// ValidateV39Restore is read-only. It neither claims restore ownership, prepares
// counters nor bootstraps tables. The write path revalidates all inputs/schema.
func (r Repository) ValidateV39Restore(ctx context.Context, data []byte) (RestoreValidation, error) {
	p, e := PrepareV39Restore(data)
	if e != nil {
		return RestoreValidation{}, e
	}
	if e = validateRestoreScalars(p); e != nil {
		return RestoreValidation{}, e
	}
	if r.DB == nil {
		return RestoreValidation{}, errors.New("restore storage unavailable")
	}
	if _, e = completeRestoreInventory(ctx, r.DB); e != nil {
		return RestoreValidation{}, e
	}
	return RestoreValidation{Tables: len(p.Tables), Rows: p.Rows, Warnings: p.Warnings}, nil
}

// RestoreCatalogCounts matches source companies.length and agencies?.length||0,
// including UTF16 string length and a truthy object length property.
func RestoreCatalogCounts(raw []byte) (int, json.RawMessage) {
	catalog, e := orderedJSON(raw)
	if e != nil {
		return 0, json.RawMessage("0")
	}
	companies := catalog.field("companies")
	count := 0
	if companies != nil && companies.kind == '[' {
		count = len(companies.items)
	}
	agencies := catalog.field("agencies")
	length := &jsonValue{kind: 'f'}
	if agencies != nil {
		switch agencies.kind {
		case '[':
			length.number = float64(len(agencies.items))
		case 's':
			length.number = float64(len(agencies.text))
		case '{':
			if v := agencies.field("length"); v != nil {
				truthy := v.kind != 'n' && !(v.kind == 'b' && !v.boolean) && !(v.kind == 'f' && v.number == 0) && !(v.kind == 's' && len(v.text) == 0)
				if truthy {
					length = v
				}
			}
		}
	}
	return count, rawValue(length)
}
