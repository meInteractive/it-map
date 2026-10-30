package aiproviders

import (
	_ "embed"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const RequestedDefault = "gpt-6.1-luna"

var recommended = []string{"gpt-6-luna", "gpt-5.6-terra", "gpt-6.1-sol", "gpt-6-sol", "gpt-5.6-sol"}

//go:embed model_profiles.json
var modelProfilesJSON []byte

type Model struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	ReasoningEfforts []string        `json:"reasoningEfforts"`
	Recommended      bool            `json:"recommended"`
	Pricing          json.RawMessage `json:"pricing"`
}

var textModel = regexp.MustCompile(`^(?:gpt-(?:[56](?:[.-]|$)|4\.1(?:-|$)|4o(?:-|$))|chatgpt-[56](?:[.-]|$)|o[134](?:-|$)|ft:(?:gpt-(?:[56]|4\.1|4o)|o[134]))`)
var excludedModel = regexp.MustCompile(`(?i)(?:audio|realtime|transcrib|tts|image|search|deep-research|computer-use|instruct|moderation|embedding)`)

func TextModel(id string) bool {
	return len(utf16.Encode([]rune(id))) <= 160 && textModel.MatchString(id) && !excludedModel.MatchString(id)
}
func ModelProfile(id string) Model {
	var known map[string]Model
	_ = json.Unmarshal(modelProfilesJSON, &known)
	if m, ok := known[id]; ok {
		return m
	}
	m := Model{ID: id, Name: id, ReasoningEfforts: []string{}, Pricing: json.RawMessage("null")}
	for base, v := range known {
		if len(v.ReasoningEfforts) > 0 && regexp.MustCompile("^"+regexp.QuoteMeta(base)+`-\d{4}-\d{2}-\d{2}$`).MatchString(id) {
			m.Name = v.Name
			m.ReasoningEfforts = v.ReasoningEfforts
			break
		}
	}
	return m
}
func DefaultModel(models []Model) string {
	for _, id := range append([]string{RequestedDefault}, recommended...) {
		for _, m := range models {
			if m.ID == id {
				return id
			}
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}
func OpenAIModels(raw json.RawMessage) ([]Model, error) {
	var data struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Data == nil {
		return nil, ErrStorage
	}
	seen := map[string]bool{}
	models := []Model{}
	for _, row := range data.Data {
		var id string
		if json.Unmarshal(row["id"], &id) != nil || !TextModel(id) || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, ModelProfile(id))
	}
	priority := append([]string{RequestedDefault}, recommended...)
	rank := func(id string) int {
		for i, x := range priority {
			if id == x {
				return i
			}
		}
		return len(priority)
	}
	col := collate.New(language.English)
	sort.SliceStable(models, func(i, j int) bool {
		a, b := rank(models[i].ID), rank(models[j].ID)
		if a != b {
			return a < b
		}
		return col.CompareString(models[i].ID, models[j].ID) < 0
	})
	return models, nil
}
func number(v any) *float64 {
	var n float64
	var e error
	switch v := v.(type) {
	case float64:
		n = v
	case string:
		s := strings.TrimFunc(v, func(r rune) bool {
			return r == 0xfeff || r == '\u00a0' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == '\u1680' || r >= '\u2000' && r <= '\u200a' || r == '\u2028' || r == '\u2029' || r == '\u202f' || r == '\u205f' || r == '\u3000'
		})
		if s == "" {
			return nil
		}
		if len(s) > 2 && s[0] == '0' && strings.ContainsAny(s[1:2], "xXbBoO") {
			var i uint64
			i, e = strconv.ParseUint(s, 0, 64)
			n = float64(i)
		} else {
			n, e = strconv.ParseFloat(s, 64)
		}
	default:
		return nil
	}
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func includes(v any, want string) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range a {
		if x == want {
			return true
		}
	}
	return false
}
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func text(v any) string { s, _ := v.(string); return s }
func numericValue(v any) any {
	n := number(v)
	if n == nil {
		return nil
	}
	return *n
}
func baseID(s string) string { return strings.TrimSuffix(s, ":free") }
func RankRouterModels(models, benchmarks []map[string]any, at string) []map[string]any {
	bySlug := map[string]map[string]any{}
	for _, b := range benchmarks {
		if b["source"] == "artificial-analysis" {
			bySlug[text(b["model_permaslug"])] = b
		}
	}
	rows := []map[string]any{}
	seen := map[string]bool{}
	for _, m := range models {
		id, ok := m["id"].(string)
		if !ok || id == "openrouter/free" || seen[id] {
			continue
		}
		seen[id] = true
		ctx := number(m["context_length"])
		price := object(m["pricing"])
		architecture := object(m["architecture"])
		if ctx == nil || *ctx < 262144 || !includes(m["supported_parameters"], "reasoning") || !includes(architecture["output_modalities"], "text") {
			continue
		}
		valid := true
		for _, k := range []string{"prompt", "completion", "request"} {
			v, found := price[k]
			if k == "request" && !found {
				continue
			}
			n := number(v)
			if n == nil || *n != 0 {
				valid = false
			}
		}
		if !valid {
			continue
		}
		slug := text(m["canonical_slug"])
		b := map[string]any{}
		for _, candidate := range []string{id, slug, baseID(id), baseID(slug)} {
			if hit, found := bySlug[candidate]; found {
				b = hit
				break
			}
		}
		name, ok := m["name"].(string)
		if !ok {
			name = id
		}
		if slug == "" {
			slug = id
		}
		pricing := map[string]any{"prompt": 0, "completion": 0}
		if _, ok := price["request"]; ok {
			pricing["request"] = 0
		}
		rows = append(rows, map[string]any{"id": id, "name": name, "canonical_slug": slug, "context_length": *ctx, "pricing": pricing, "supports_reasoning": true, "supports_structured_outputs": includes(m["supported_parameters"], "structured_outputs"), "intelligence_index": numericValue(b["intelligence_index"]), "agentic_index": numericValue(b["agentic_index"]), "coding_index": numericValue(b["coding_index"]), "is_eligible": true, "fetched_at": at})
	}
	col := collate.New(language.English)
	sort.SliceStable(rows, func(i, j int) bool {
		for _, k := range []string{"intelligence_index", "agentic_index", "coding_index", "context_length"} {
			a, b := rows[i][k], rows[j][k]
			if a == b {
				continue
			}
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			return a.(float64) > b.(float64)
		}
		return col.CompareString(text(rows[i]["id"]), text(rows[j]["id"])) < 0
	})
	for i, m := range rows {
		m["rank"] = i + 1
	}
	return rows
}
