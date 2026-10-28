package dbschema

import (
	"encoding/json"
	"testing"
)

func copySnapshot(t *testing.T, in Snapshot) Snapshot {
	t.Helper()
	b, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	var out Snapshot
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func verifiedPolicy(t *testing.T) Policy {
	t.Helper()
	p := Trusted()
	if len(p.Metadata) == 0 {
		t.Skip("awaiting required disposable server metadata capture")
	}
	return p
}
func removeObject(s Snapshot, section, table, name string) {
	rows := s[section]
	s[section] = [][]*string{}
	for _, row := range rows {
		if cell(row, 0) != table || cell(row, 1) != name {
			s[section] = append(s[section], row)
		}
	}
}
func TestPreflightRefusesMetadataChanges(t *testing.T) {
	p := verifiedPolicy(t)
	for section, rows := range p.Metadata {
		if len(rows) == 0 {
			t.Fatal("missing metadata section", section)
		}
		for col := range rows[0] {
			t.Run(section+" changed field "+string(rune('a'+col)), func(t *testing.T) {
				s := copySnapshot(t, p.Metadata)
				changed := "CI incompatible schema"
				s[section][0][col] = &changed
				if actions, e := Preflight(s); e == nil || actions != nil {
					t.Fatal("changed metadata authorized DDL")
				}
			})
		}
	}
	wrongSections := copySnapshot(t, p.Metadata)
	wrongSections["CI unknown"] = wrongSections["tables"]
	delete(wrongSections, "tables")
	if _, e := Preflight(wrongSections); e == nil {
		t.Fatal("unknown metadata section replaced a required section")
	}
	for _, section := range []string{"columns", "checks"} {
		t.Run("missing existing "+section, func(t *testing.T) {
			s := copySnapshot(t, p.Metadata)
			s[section] = s[section][1:]
			if _, e := Preflight(s); e == nil {
				t.Fatal("damaged existing table accepted")
			}
		})
	}
	for _, section := range []string{"indexes", "foreign_keys"} {
		t.Run("torn "+section, func(t *testing.T) {
			s := copySnapshot(t, p.Metadata)
			found := false
			for i, row := range s[section] {
				for j, other := range s[section] {
					if i != j && cell(row, 0) == cell(other, 0) && cell(row, 1) == cell(other, 1) {
						s[section] = append(s[section][:i], s[section][i+1:]...)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				t.Skip("no composite object in this metadata section")
			}
			s["triggers"] = s["triggers"][1:] // another missing object must not hide damage
			if _, e := Preflight(s); e == nil {
				t.Fatal("torn composite authorized unrelated DDL")
			}
		})
	}
}
func TestPreflightPlansOnlyMissingObjects(t *testing.T) {
	p := verifiedPolicy(t)
	if e := CheckFull(p.Metadata); e != nil {
		t.Fatal(e)
	}
	for kind, section := range map[string]string{"index": "indexes", "foreign_key": "foreign_keys", "trigger": "triggers"} {
		t.Run(kind, func(t *testing.T) {
			var expected Action
			for _, a := range p.Actions {
				if a.Kind == kind {
					expected = a
					break
				}
			}
			s := copySnapshot(t, p.Metadata)
			removeObject(s, section, expected.Table, expected.Name)
			actions, e := Preflight(s)
			if e != nil || len(actions) != 1 || actions[0] != expected {
				t.Fatalf("missing object plan differs: %v %v", actions, e)
			}
			if e = CheckFull(s); e == nil {
				t.Fatal("partial schema reported ready")
			}
		})
	}
	empty := Snapshot{}
	for section := range p.Metadata {
		empty[section] = [][]*string{}
	}
	actions, e := Preflight(empty)
	if e != nil || len(actions) != 217 {
		t.Fatal("new empty database must receive all trusted actions", e)
	}
	// A table left by an interrupted bootstrap is preserved when its complete
	// definition matches. No DROP/ALTER rewrite is generated for that table.
	s := copySnapshot(t, empty)
	for section, rows := range p.Metadata {
		for _, row := range rows {
			if cell(row, 0) == "agency_identity" {
				s[section] = append(s[section], row)
			}
		}
	}
	actions, e = Preflight(s)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range actions {
		if a.Kind == "table" && a.Table == "agency_identity" {
			t.Fatal("valid existing table recreated")
		}
	}
	for _, a := range actions {
		if a.SQL == "" {
			t.Fatal("empty DDL")
		}
	}
	if e = CheckFull(s); e == nil {
		t.Fatal("partial database reported ready")
	}
}
