// Package parser holds migration contracts. No scheduler is started here.
package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
)

var ErrSettingsInput = errors.New("invalid parser settings input")

type SettingsError struct{ Message string }

func (e *SettingsError) Error() string { return e.Message }
func (e *SettingsError) Unwrap() error { return ErrSettingsInput }

const settingsMessage = "Передайте enabled, дневной/месячный бюджет USD (0–10000) и revision."
const sourcesMessage = "Выберите поддерживаемые источники без повторов, период 1/3/7 дней и область компаний."
const prioritiesObjectMessage = "Приоритеты должны быть объектом."
const prioritiesMessage = "Приоритеты: целые0–100 для всех типов."
const maxSafeInteger = 9007199254740991

type Settings struct {
	Enabled         bool             `json:"enabled"`
	AgenciesEnabled bool             `json:"agenciesEnabled"`
	OwnerName       string           `json:"ownerName"`
	DayBudgetUSD    float64          `json:"dayBudgetUsd"`
	MonthBudgetUSD  float64          `json:"monthBudgetUsd"`
	Revision        int64            `json:"revision"`
	SourceIDs       []string         `json:"sourceIds"`
	IntervalDays    int              `json:"intervalDays"`
	Priorities      map[string]int64 `json:"priorities"`
	FavoritesOnly   bool             `json:"favoritesOnly"`
	CloudConnected  bool             `json:"cloudConnected"`
}
type SettingsUpdate struct {
	Next             Settings `json:"next"`
	ExpectedRevision int64    `json:"expectedRevision"`
}
type SourceOption struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Supported bool    `json:"supported"`
	Reason    *string `json:"reason"`
}

func SourceOptions() []SourceOption {
	reason := func(s string) *string { return &s }
	return []SourceOption{{"companySite", "Сайт компании", true, nil}, {"hh", "HH", true, nil}, {"habr", "Хабр", true, nil}, {"getmatch", "Getmatch", true, nil}, {"telegram", "Telegram", false, reason("Telegram не поддерживается этим сборщиком.")}, {"dreamJob", "DreamJob", false, reason("Нужен отдельный адаптер списка вакансий.")}, {"hCareers", "Эйч", false, reason("Нужен отдельный адаптер списка вакансий.")}, {"rabota", "rabota.ru", true, nil}}
}
func DefaultSettings() Settings {
	return Settings{OwnerName: "Eugene", SourceIDs: []string{}, IntervalDays: 7, FavoritesOnly: true, Priorities: map[string]int64{"import_vacancy": 100, "assign_roles": 90, "score_relevance": 85, "extract_vacancy": 80, "fetch_full_text": 70, "classify_title": 60, "scan_source": 20, "diagnostic": 0}}
}

// readObject also rejects duplicate keys: the accepted input boundary is stricter
// than JSON.parse's last-wins behavior. Values remain raw for exact type checks.
func readObject(data []byte) (map[string]json.RawMessage, error) {
	if len(data) > 64*1024 {
		return nil, ErrSettingsInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrSettingsInput
	}
	out := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, ErrSettingsInput
		}
		key, ok := token.(string)
		if !ok {
			return nil, ErrSettingsInput
		}
		if _, exists := out[key]; exists {
			return nil, ErrSettingsInput
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, ErrSettingsInput
		}
		out[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, ErrSettingsInput
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrSettingsInput
	}
	return out, nil
}
func number(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}
func integer(raw json.RawMessage) (int64, bool) {
	n, ok := number(raw)
	if !ok || math.Abs(n) > maxSafeInteger || math.Trunc(n) != n {
		return 0, false
	}
	return int64(n), true
}
func boolean(raw json.RawMessage) (bool, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
		return true, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
		return false, true
	}
	return false, false
}

// ValidateSettingsUpdate performs no SQL and grants no permission to start work.
// ExpectedRevision must be used by the authorized CAS write, not ignored.
// Owner comes from the caller's authenticated context, never the JSON payload.
func ValidateSettingsUpdate(data []byte, previous Settings, ownerName string) (SettingsUpdate, error) {
	fail := func(message string) (SettingsUpdate, error) {
		return SettingsUpdate{}, &SettingsError{Message: message}
	}
	raw, err := readObject(data)
	if err != nil {
		return fail(settingsMessage)
	}
	for key := range raw {
		switch key {
		case "enabled", "dayBudgetUsd", "monthBudgetUsd", "revision", "priorities", "favoritesOnly", "sourceIds", "intervalDays", "agenciesEnabled":
		default:
			return fail(settingsMessage)
		}
	}
	enabled, ok := boolean(raw["enabled"])
	revision, revOK := integer(raw["revision"])
	day, dayOK := number(raw["dayBudgetUsd"])
	month, monthOK := number(raw["monthBudgetUsd"])
	if !ok || !revOK || !dayOK || !monthOK || day < 0 || day > 10000 || month < 0 || month > 10000 {
		return fail(settingsMessage)
	}
	next := previous
	next.Enabled = enabled
	next.DayBudgetUSD = day
	next.MonthBudgetUSD = month
	next.Revision = previous.Revision + 1
	next.OwnerName = ownerName
	next.SourceIDs = append([]string{}, previous.SourceIDs...)
	next.Priorities = map[string]int64{}
	for k, v := range previous.Priorities {
		next.Priorities[k] = v
	}
	for _, field := range []string{"agenciesEnabled", "favoritesOnly"} {
		if value, present := raw[field]; present {
			v, valid := boolean(value)
			if !valid {
				return fail(sourcesMessage)
			}
			if field == "agenciesEnabled" {
				next.AgenciesEnabled = v
			} else {
				next.FavoritesOnly = v
			}
		}
	}
	if value, present := raw["intervalDays"]; present {
		interval, valid := integer(value)
		if !valid || (interval != 1 && interval != 3 && interval != 7) {
			return fail(sourcesMessage)
		}
		next.IntervalDays = int(interval)
	}
	if value, present := raw["sourceIds"]; present {
		var ids []string
		options := SourceOptions()
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &ids) != nil || len(ids) > len(options) {
			return fail(sourcesMessage)
		}
		supported := map[string]bool{}
		for _, v := range options {
			supported[v.ID] = v.Supported
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !supported[id] || seen[id] {
				return fail(sourcesMessage)
			}
			seen[id] = true
		}
		next.SourceIDs = append([]string{}, ids...)
	}
	if value, present := raw["priorities"]; present && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		priorities, err := readObject(value)
		if err != nil {
			return fail(prioritiesObjectMessage)
		}
		for k, value := range priorities {
			v, valid := integer(value)
			if !valid || v < 0 || v > 100 {
				return fail(prioritiesMessage)
			}
			next.Priorities[k] = v
		}
	}
	known := DefaultSettings().Priorities
	if len(next.Priorities) != len(known) {
		return fail(prioritiesMessage)
	}
	for k := range known {
		v, present := next.Priorities[k]
		if !present || v < 0 || v > 100 {
			return fail(prioritiesMessage)
		}
	}
	for k := range next.Priorities {
		if _, present := known[k]; !present {
			return fail(prioritiesMessage)
		}
	}
	return SettingsUpdate{Next: next, ExpectedRevision: revision}, nil
}
