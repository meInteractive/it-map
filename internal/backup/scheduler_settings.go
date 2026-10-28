package backup

import (
	"encoding/json"
	"errors"
	"math"

	"itmap/internal/v39"
)

var ErrSchedulerSettingsView = errors.New("invalid stored scheduler settings projection")

// SchedulerSettingsView owns the effective source state. Its private member is
// deliberately not exported; generic JSON serialization always redacts the owner.
// This view performs no input validation, SQL, restore reset or provider work.
type SchedulerSettingsView struct{ value *jsonValue }

// ReadSchedulerSettingsJSON follows schedulerSettings on an already selected row.
// legacySources is the preselected DISTINCT read set, not a second SQL engine.
func ReadSchedulerSettingsJSON(raw []byte, favoritesOnly bool, legacySources []string) (SchedulerSettingsView, error) {
	saved, e := orderedJSON(raw)
	if e != nil || saved.kind != '{' && saved.kind != '[' {
		return SchedulerSettingsView{}, ErrSchedulerSettingsView
	}
	rules := v39.RestorePolicy()
	value, e := orderedJSON(rules.SchedulerDefaults)
	if e != nil {
		return SchedulerSettingsView{}, ErrSchedulerSettingsView
	}
	priorities := catalogObject()
	catalogSpread(priorities, value.field("priorities"))
	catalogSpread(priorities, saved.field("priorities"))
	catalogSpread(value, saved)
	if saved.field("sourceIds") == nil {
		allowed := map[string]bool{}
		for _, id := range rules.SupportedSources {
			allowed[id] = true
		}
		ids := &jsonValue{kind: '[', items: []*jsonValue{}}
		for _, id := range legacySources {
			if allowed[id] {
				ids.items = append(ids.items, catalogString(id))
			}
		}
		value.set("sourceIds", ids)
	}
	value.set("priorities", priorities)
	value.set("favoritesOnly", readBool(favoritesOnly))
	return SchedulerSettingsView{value: catalogOrderedValue(value)}, nil
}

func publicSchedulerValue(value *jsonValue) *jsonValue {
	if value == nil {
		return catalogNull()
	}
	out := catalogObject()
	catalogSpread(out, value)
	connected := jsonTruthy(value.field("cloudOwnerId"))
	catalogDelete(out, "cloudOwnerId")
	out.set("cloudConnected", readBool(connected))
	return catalogOrderedValue(out)
}
func (v SchedulerSettingsView) PublicJSON() json.RawMessage {
	return rawValue(publicSchedulerValue(v.value))
}
func (v SchedulerSettingsView) MarshalJSON() ([]byte, error) { return v.PublicJSON(), nil }

// SchedulerSettingsChange separates private storage text from the public DTO.
// MarshalJSON exposes only the public DTO, including if a caller serializes the
// whole change by mistake. Returned byte slices never alias owned state.
type SchedulerSettingsChange struct{ stored, public *jsonValue }

func (v SchedulerSettingsChange) PublicJSON() json.RawMessage {
	if v.public == nil {
		return json.RawMessage("null")
	}
	return rawValue(v.public)
}
func (v SchedulerSettingsChange) StorageJSON() json.RawMessage { return rawValue(v.stored) }
func (v SchedulerSettingsChange) MarshalJSON() ([]byte, error) { return v.PublicJSON(), nil }

func schedulerRevisionIncrement(value *jsonValue) (*jsonValue, error) {
	if value == nil {
		return catalogNumber(math.NaN()), nil
	}
	switch value.kind {
	case 'f':
		return catalogNumber(value.number + 1), nil
	case 'n':
		return catalogNumber(1), nil
	case 'b':
		if value.boolean {
			return catalogNumber(2), nil
		}
		return catalogNumber(1), nil
	default:
		n := &catalogNormalizer{}
		text := n.str(value)
		if n.err != nil {
			return nil, ErrSchedulerSettingsView
		}
		return catalogText(append(append([]uint16{}, text...), '1')), nil
	}
}

// NextFromValidated serializes the unchanged source normalization after the
// existing parser.ValidateSettingsUpdate gate. It intentionally does not invent
// another business validator or execute the expected-revision SQL/CAS operation.
// Unknown stored members and cloudOwnerId survive; input cannot replace them.
func (v SchedulerSettingsView) NextFromValidated(input json.RawMessage, ownerName string) (SchedulerSettingsChange, error) {
	raw, e := orderedJSON(input)
	if e != nil || raw.kind != '{' || v.value == nil {
		return SchedulerSettingsChange{}, ErrSchedulerSettingsView
	}
	for _, key := range []string{"enabled", "dayBudgetUsd", "monthBudgetUsd", "revision"} {
		if raw.field(key) == nil {
			return SchedulerSettingsChange{}, ErrSchedulerSettingsView
		}
	}
	old := v.value
	next := catalogObject()
	catalogSpread(next, old)
	catalogDelete(next, "favoritesOnly")
	next.set("enabled", raw.field("enabled"))
	for _, key := range []string{"agenciesEnabled", "sourceIds", "intervalDays"} {
		value := raw.field(key)
		if jsNullish(value) {
			value = old.field(key)
		}
		next.set(key, value)
	}
	next.set("ownerName", catalogString(ownerName))
	next.set("dayBudgetUsd", raw.field("dayBudgetUsd"))
	next.set("monthBudgetUsd", raw.field("monthBudgetUsd"))
	priorities := old.field("priorities")
	if jsonTruthy(raw.field("priorities")) {
		priorities = catalogObject()
		catalogSpread(priorities, old.field("priorities"))
		catalogSpread(priorities, raw.field("priorities"))
	}
	next.set("priorities", priorities)
	revision, e := schedulerRevisionIncrement(old.field("revision"))
	if e != nil {
		return SchedulerSettingsChange{}, e
	}
	next.set("revision", revision)
	stored := catalogOrderedValue(next)
	withFavorite := catalogObject()
	catalogSpread(withFavorite, stored)
	favorite := raw.field("favoritesOnly")
	if jsNullish(favorite) {
		favorite = old.field("favoritesOnly")
	}
	withFavorite.set("favoritesOnly", favorite)
	return SchedulerSettingsChange{stored: stored, public: publicSchedulerValue(withFavorite)}, nil
}
