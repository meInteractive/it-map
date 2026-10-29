package backup

import (
	"encoding/json"
	"errors"
	"itmap/internal/v39"
	"math"
	"unicode/utf16"
)

var ErrSchedulerQueueFilters = errors.New("invalid scheduler queue filters")
var ErrSchedulerQueueKeys = errors.New("unknown scheduler queue query key")

type SchedulerQueueFilters struct {
	Page                              int64
	Status, Type, CompanyID, AgencyID string
	needle                            []uint16
}

// PrepareSchedulerQueueFilters сохраняет source Number/UTF16 length/trim/lower.
// ID остаётся текстом: SQL affinity SQLite отличается от допуска через Number.
func PrepareSchedulerQueueFilters(raw json.RawMessage) (SchedulerQueueFilters, error) {
	f := SchedulerQueueFilters{Page: 1}
	root, e := orderedJSON(raw)
	if e != nil || root.kind != '{' {
		return f, ErrSchedulerQueueFilters
	}
	for _, m := range root.members {
		key := stringFromUnits(m.key)
		switch key {
		case "page", "status", "type", "companyId", "agencyId", "search":
		default:
			return f, ErrSchedulerQueueKeys
		}
		if m.value.kind != 's' {
			return f, ErrSchedulerQueueFilters
		}
	}
	if p := root.field("page"); p != nil {
		n := legacyNumber(p)
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < 1 || n > 100000 {
			return f, ErrSchedulerQueueFilters
		}
		f.Page = int64(n)
	}
	f.Status = textField(root, "status")
	f.Type = textField(root, "type")
	f.CompanyID = textField(root, "companyId")
	f.AgencyID = textField(root, "agencyId")
	if f.Status != "" && f.Status != "pending" && f.Status != "running" && f.Status != "completed" && f.Status != "failed" && f.Status != "cancelled" {
		return f, ErrSchedulerQueueFilters
	}
	if f.Type != "" {
		defaults, e := orderedJSON(v39.RestorePolicy().SchedulerDefaults)
		if e != nil || defaults.field("priorities").field(f.Type) == nil {
			return f, ErrSchedulerQueueFilters
		}
	}
	for _, key := range []string{"companyId", "agencyId"} {
		v := root.field(key)
		if v == nil || len(v.text) == 0 {
			continue
		}
		n := legacyNumber(v)
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < 1 || n > 9007199254740991 {
			return f, ErrSchedulerQueueFilters
		}
	}
	if f.CompanyID != "" && f.AgencyID != "" {
		return f, ErrSchedulerQueueFilters
	}
	if s := root.field("search"); s != nil {
		if len(s.text) > 200 {
			return f, ErrSchedulerQueueFilters
		}
		f.needle = roleLower(trimUnits(s.text))
	}
	return f, nil
}
func (f SchedulerQueueFilters) HasSearch() bool { return len(f.needle) != 0 }
func (f SchedulerQueueFilters) MatchOwner(name string) bool {
	return catalogSearchIncludes(roleLower(utf16.Encode([]rune(name))), f.needle, catalogSearchPrefix(f.needle))
}
