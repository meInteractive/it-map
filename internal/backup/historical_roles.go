package backup

import (
	"encoding/hex"
	"encoding/json"
	"itmap/internal/v39"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

type historicalRules struct {
	Warnings         map[int][]string             `json:"warnings"`
	Roles            []map[string]json.RawMessage `json:"roles"`
	Lower            map[int]string               `json:"lower"`
	Cased, Ignorable [][2]int
}

var historicalPolicy = sync.OnceValue(func() *historicalRules {
	var p historicalRules
	if e := json.Unmarshal(v39.HistoricalPolicy(), &p); e != nil {
		panic("invalid trusted historical policy")
	}
	return &p
})

func jsWhite(u uint16) bool {
	return u >= 9 && u <= 13 || u == 32 || u == 0xa0 || u == 0x1680 || u >= 0x2000 && u <= 0x200a || u == 0x2028 || u == 0x2029 || u == 0x202f || u == 0x205f || u == 0x3000 || u == 0xfeff
}
func trimUnits(s []uint16) []uint16 {
	a, b := 0, len(s)
	for a < b && jsWhite(s[a]) {
		a++
	}
	for b > a && jsWhite(s[b-1]) {
		b--
	}
	return s[a:b]
}
func unitsKey(s []uint16) string {
	b := make([]byte, 0, len(s)*2)
	for _, u := range s {
		b = append(b, byte(u>>8), byte(u))
	}
	return string(b)
}
func inUnicodeRanges(cp int, ranges [][2]int) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i][1] >= cp })
	return i < len(ranges) && ranges[i][0] <= cp
}
func roleLower(s []uint16) []uint16 {
	p := historicalPolicy()
	cps := make([]int, 0, len(s))
	for i := 0; i < len(s); i++ {
		cp := int(s[i])
		if s[i] >= 0xd800 && s[i] <= 0xdbff && i+1 < len(s) && s[i+1] >= 0xdc00 && s[i+1] <= 0xdfff {
			cp = 0x10000 + (int(s[i])-0xd800)*1024 + int(s[i+1]) - 0xdc00
			i++
		}
		cps = append(cps, cp)
	}
	out := make([]uint16, 0, len(s))
	for i, cp := range cps {
		if cp == 0x3a3 {
			before, after := false, false
			for j := i - 1; j >= 0; j-- {
				if inUnicodeRanges(cps[j], p.Ignorable) {
					continue
				}
				before = inUnicodeRanges(cps[j], p.Cased)
				break
			}
			for j := i + 1; j < len(cps); j++ {
				if inUnicodeRanges(cps[j], p.Ignorable) {
					continue
				}
				after = inUnicodeRanges(cps[j], p.Cased)
				break
			}
			if before && !after {
				out = append(out, 0x3c2)
				continue
			}
		}
		if lower, ok := p.Lower[cp]; ok {
			out = append(out, utf16.Encode([]rune(lower))...)
		} else if cp <= 0xffff {
			out = append(out, uint16(cp))
		} else {
			hi, lo := utf16.EncodeRune(rune(cp))
			out = append(out, uint16(hi), uint16(lo))
		}
	}
	return out
}
func normalizedRole(s []uint16) string {
	out := []uint16{}
	white := false
	for _, u := range trimUnits(s) {
		if jsWhite(u) {
			if !white {
				out = append(out, 32)
			}
			white = true
		} else {
			white = false
			out = append(out, u)
		}
	}
	return unitsKey(roleLower(out))
}
func jsonTruthy(v *jsonValue) bool {
	return v != nil && v.kind != 'n' && !(v.kind == 'b' && !v.boolean) && !(v.kind == 'f' && v.number == 0) && !(v.kind == 's' && len(v.text) == 0)
}
func legacyRoleText(raw json.RawMessage) []uint16 {
	v, _ := orderedJSON(raw)
	if !jsonTruthy(v) {
		return nil
	}
	if v.kind == 's' {
		return v.text
	}
	return utf16.Encode([]rune(string(v.stringify(nil, false))))
}
func rawText(s []uint16) json.RawMessage {
	return (&jsonValue{kind: 's', text: s}).stringify(nil, false)
}
func rawString(s string) json.RawMessage { return rawText(utf16.Encode([]rune(s))) }
func rawInteger(n int) json.RawMessage   { return json.RawMessage(strconv.Itoa(n)) }
func cloneRow(r map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range r {
		node, e := orderedJSON(v)
		if e == nil {
			out[k] = rawValue(node)
		} else {
			out[k] = append(json.RawMessage(nil), v...)
		}
	}
	return out
}
func cloneRows(rows []map[string]json.RawMessage) []map[string]json.RawMessage {
	out := make([]map[string]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, cloneRow(r))
	}
	return out
}
func roleID(r map[string]json.RawMessage, name string) int {
	var n int
	_ = json.Unmarshal(r[name], &n)
	return n
}

type roleReference struct {
	id, parent int
	key        string
	leaf       bool
}
type roleResolver struct {
	byKey map[string]roleReference
	byID  map[int]roleReference
	names map[string][]int
}

func newRoleResolver(roles []map[string]json.RawMessage) *roleResolver {
	refs := map[int]roleReference{}
	parents := map[int]bool{}
	for _, r := range roles {
		parents[roleID(r, "parent_id")] = true
	}
	result := &roleResolver{byKey: map[string]roleReference{}, byID: refs, names: map[string][]int{}}
	for _, r := range roles {
		id := roleID(r, "id")
		var key string
		_ = json.Unmarshal(r["stable_key"], &key)
		ref := roleReference{id: id, parent: roleID(r, "parent_id"), key: key, leaf: !parents[id]}
		refs[id] = ref
		result.byKey[key] = ref
		if ref.leaf {
			name := normalizedRole(legacyRoleText(r["name"]))
			result.names[name] = append(result.names[name], id)
		}
	}
	return result
}

var legacyRoleKeys = map[string]string{"менеджер проектов": "it-project-manager", "менеджер продукта": "it-product-manager", "менеджер продуктов": "it-product-manager", "руководитель разработки": "it-development-manager", "директор по разработке": "it-development-director", "редактор": "journalist-editor", "дата-журналист": "journalist-data", "it manager": "it-unspecified", "журналист": "journalist-unspecified"}

func (r *roleResolver) resolve(value []uint16) []int {
	parts := [][]uint16{}
	start := 0
	for i, u := range value {
		if u == ';' || u == ',' {
			parts = append(parts, trimUnits(value[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, trimUnits(value[start:]))
	ids := []int{}
	seen := map[int]bool{}
	valid := true
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		token := part
		for i, u := range part {
			if u == 0x2192 {
				token = part[i+1:]
			}
		}
		normalized := normalizedRole(token)
		key := ""
		for text, k := range legacyRoleKeys {
			if normalized == unitsKey(utf16.Encode([]rune(text))) {
				key = k
				break
			}
		}
		ref, ok := r.byKey[key]
		root := "root-it-manager"
		if strings.HasPrefix(ref.key, "journalist-") {
			root = "root-journalist"
		}
		id := 0
		if ok && ref.leaf && r.byID[ref.parent].key == root {
			id = ref.id
		} else if matched := r.names[normalizedRole(part)]; len(matched) == 1 {
			id = matched[0]
		}
		if id == 0 {
			valid = false
			break
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if valid && len(ids) > 0 {
		return ids
	}
	if matched := r.names[normalizedRole(value)]; len(matched) == 1 {
		return []int{matched[0]}
	}
	return nil
}
func migratedLegacyRoles(vacancies []map[string]json.RawMessage, date json.RawMessage) ([]map[string]json.RawMessage, []map[string]json.RawMessage) {
	roles := cloneRows(historicalPolicy().Roles)
	for _, r := range roles {
		r["created_at"] = date
		r["updated_at"] = date
	}
	values := [][]uint16{}
	seen := map[string]bool{}
	for _, r := range vacancies {
		value := trimUnits(legacyRoleText(r["role"]))
		key := unitsKey(value)
		if len(value) > 0 && !seen[key] {
			seen[key] = true
			values = append(values, value)
		}
	}
	next := 9
	add := func(key string, name []uint16, parent json.RawMessage, order int) {
		roles = append(roles, map[string]json.RawMessage{"id": rawInteger(next), "stable_key": rawString(key), "name": rawText(name), "parent_id": parent, "sort_order": rawInteger(order), "created_at": date, "updated_at": date})
		next++
	}
	for _, spec := range []struct {
		lower, key, name string
		parent           int
	}{{"it manager", "it-unspecified", "Без уточнения (IT Manager)", 1}, {"журналист", "journalist-unspecified", "Без уточнения (Журналист)", 2}} {
		for _, v := range values {
			if unitsKey(roleLower(v)) == unitsKey(utf16.Encode([]rune(spec.lower))) {
				add(spec.key, utf16.Encode([]rune(spec.name)), rawInteger(spec.parent), 90)
				break
			}
		}
	}
	resolver := newRoleResolver(roles)
	unknown := [][]uint16{}
	for _, v := range values {
		if resolver.resolve(v) == nil {
			unknown = append(unknown, v)
		}
	}
	if len(unknown) > 0 {
		parent := next
		add("root-preserved", utf16.Encode([]rune("Сохранённые роли")), json.RawMessage("null"), 90)
		for _, v := range unknown {
			add("legacy-"+hex.EncodeToString([]byte(stringFromUnits(v))), v, rawInteger(parent), 100+next)
		}
	}
	resolver = newRoleResolver(roles)
	assignments := []map[string]json.RawMessage{}
	for _, r := range vacancies {
		for _, id := range resolver.resolve(legacyRoleText(r["role"])) {
			assignments = append(assignments, map[string]json.RawMessage{"vacancy_id": r["id"], "role_id": rawInteger(id)})
		}
	}
	return roles, assignments
}
