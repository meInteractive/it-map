package backup

import (
	"encoding/json"
	"strconv"

	"itmap/internal/v39"
)

// RestorePlan owns parsed data, independent of the input bytes. SQL callers must
// validate scalar representability before opening the restore session.
// It is a data plan only: metadata SQL, counters, generation and destination
// writes are not executed here. INSERT order differs from incoming file order.
type RestorePlan struct {
	Tables                             []Table
	InsertOrder, DeleteOrder, Warnings []string
	Sequences                          []Sequence
	Catalog                            json.RawMessage
	Rows                               int
	SourceVersion                      int
}

func setJSONMember(v *jsonValue, key []uint16, value *jsonValue) {
	for i, m := range v.members {
		if len(m.key) != len(key) {
			continue
		}
		same := true
		for j, u := range key {
			if u != m.key[j] {
				same = false
				break
			}
		}
		if same {
			v.members[i].value = value
			return
		}
	}
	v.members = append(v.members, jsonMember{key: append([]uint16(nil), key...), value: value})
}
func spreadJSON(target, source *jsonValue) {
	switch source.kind {
	case '{':
		for _, m := range source.members {
			setJSONMember(target, m.key, m.value)
		}
	case '[':
		for i, v := range source.items {
			target.set(strconv.Itoa(i), v)
		}
	case 's':
		for i, u := range source.text {
			target.set(strconv.Itoa(i), &jsonValue{kind: 's', text: []uint16{u}})
		}
	}
}
func restoreScheduler(row map[string]json.RawMessage, sources []map[string]json.RawMessage, rules v39.RestoreRules) error {
	cell, err := orderedJSON(row["scheduler_json"])
	if err != nil {
		return invalid("Некорректные настройки планировщика в бэкапе.")
	}
	text := rawValue(cell)
	if cell.kind == 's' {
		text, err = jsonSourceText(cell.text)
		if err != nil {
			return err
		}
	}
	saved, err := orderedJSON(text)
	if err != nil || saved.kind == 'n' {
		return invalid("Некорректные настройки планировщика в бэкапе.")
	}
	merged, err := orderedJSON(rules.SchedulerDefaults)
	if err != nil {
		return err
	}
	spreadJSON(merged, saved)
	ids := saved.field("sourceIds")
	if ids == nil || ids.kind == 'n' {
		allowed := map[string]bool{}
		for _, id := range rules.SupportedSources {
			allowed[id] = true
		}
		seen := map[string]bool{}
		ids = &jsonValue{kind: '['}
		for _, source := range sources {
			if string(source["auto_enabled"]) != "1" {
				continue
			}
			id, e := orderedJSON(source["source_id"])
			if e != nil || id.kind != 's' {
				continue
			}
			name := stringFromUnits(id.text)
			if allowed[name] && !seen[name] {
				seen[name] = true
				ids.items = append(ids.items, id)
			}
		}
	}
	merged.set("sourceIds", ids)
	merged.set("enabled", &jsonValue{kind: 'b', boolean: false})
	merged.set("cloudOwnerId", &jsonValue{kind: 's'})
	// Reparse to apply OwnPropertyKeys numeric-index ordering after spread.
	canonical, err := orderedJSON(merged.stringify(nil, false))
	if err != nil {
		return err
	}
	quoted, err := orderedJSON(encodeScalarText(string(canonical.stringify(nil, false))))
	if err != nil {
		return err
	}
	row["scheduler_json"] = quoted.stringify(nil, false)
	return nil
}
func parentFirstTables() []string {
	tables := v39.Trusted().Tables
	byName := map[string]v39.Table{}
	for _, t := range tables {
		byName[t.Name] = t
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	ordered := make([]string, 0, len(tables))
	var visit func(v39.Table)
	visit = func(t v39.Table) {
		if visited[t.Name] || visiting[t.Name] {
			return
		}
		visiting[t.Name] = true
		for _, name := range t.ParentTables {
			if parent, ok := byName[name]; ok {
				visit(parent)
			}
		}
		delete(visiting, t.Name)
		visited[t.Name] = true
		ordered = append(ordered, t.Name)
	}
	for _, t := range tables {
		visit(t)
	}
	return ordered
}
func sortedRoleRows(rows []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	pending := append([]map[string]json.RawMessage(nil), rows...)
	ordered := make([]map[string]json.RawMessage, 0, len(rows))
	inserted := map[string]bool{}
	for len(pending) > 0 {
		ready := make([]bool, len(pending))
		any := false
		for i, r := range pending {
			parent := string(r["parent_id"])
			if parent == "null" || inserted[parent] {
				ready[i] = true
				any = true
			}
		}
		if !any {
			return nil, invalid("В справочнике ролей обнаружен цикл или отсутствующий родитель.")
		}
		remaining := make([]map[string]json.RawMessage, 0, len(pending))
		for i, r := range pending {
			if ready[i] {
				ordered = append(ordered, r)
				inserted[string(r["id"])] = true
			} else {
				remaining = append(remaining, r)
			}
		}
		pending = remaining
	}
	return ordered, nil
}

// PrepareV39Restore reproduces unchanged v39 safety transformations and ordering
// before any destination SQL. Historical versions require their own migration.
func PrepareV39Restore(data []byte) (*RestorePlan, error) {
	payload, err := ValidateV39(data)
	if err != nil {
		return nil, err
	}
	return finishRestorePlan(payload)
}

func finishRestorePlan(payload *Payload) (*RestorePlan, error) {
	var err error
	rules := v39.RestorePolicy()
	plan := &RestorePlan{SourceVersion: int(payload.Version), Tables: payload.Storage.Tables, Warnings: rules.Warnings, Sequences: payload.Metadata.Sequences, Catalog: payload.Catalog}
	byName := map[string]int{}
	for i, t := range plan.Tables {
		byName[t.Name] = i
		plan.Rows += len(t.Rows)
	}
	for _, row := range plan.Tables[byName["firecrawl_settings"]].Rows {
		if err = restoreScheduler(row, plan.Tables[byName["vacancy_source_parsing"]].Rows, rules); err != nil {
			return nil, err
		}
	}
	for _, row := range plan.Tables[byName["apify_settings"]].Rows {
		for _, field := range []string{"enabled", "paid_allowed"} {
			row[field] = json.RawMessage("0")
		}
		for _, field := range []string{"free_account_json", "starter_account_json", "free_error", "starter_error"} {
			row[field] = json.RawMessage(`""`)
		}
	}
	roles := byName["vacancy_roles"]
	plan.Tables[roles].Rows, err = sortedRoleRows(plan.Tables[roles].Rows)
	if err != nil {
		return nil, err
	}
	plan.InsertOrder = parentFirstTables()
	plan.DeleteOrder = append([]string(nil), plan.InsertOrder...)
	for i, j := 0, len(plan.DeleteOrder)-1; i < j; i, j = i+1, j-1 {
		plan.DeleteOrder[i], plan.DeleteOrder[j] = plan.DeleteOrder[j], plan.DeleteOrder[i]
	}
	return plan, nil
}
