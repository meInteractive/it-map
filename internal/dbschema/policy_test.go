package dbschema

import (
	"errors"
	"os/exec"
	"testing"
)

func TestTrustedDDLSource(t *testing.T) {
	if out, e := exec.Command("python3", "../../scripts/trusted-schema-policy.py").CombinedOutput(); e != nil {
		t.Fatalf("trusted DDL source drift: %v %s", e, out)
	}
	p := Trusted()
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, a := range p.Actions {
		key := identity(a.Kind, a.Table, a.Name)
		if seen[key] || a.SQL == "" {
			t.Fatal("duplicate/empty DDL action")
		}
		seen[key] = true
		counts[a.Kind]++
	}
	for kind, n := range map[string]int{"table": 72, "index": 73, "foreign_key": 50, "trigger": 22} {
		if counts[kind] != n {
			t.Fatal("trusted object coverage differs")
		}
	}
	p.Actions[0].SQL = "CI changed"
	if Trusted().Actions[0].SQL == "CI changed" {
		t.Fatal("caller mutated trusted DDL")
	}
	if len(p.Metadata) == 0 {
		if _, e := Preflight(Snapshot{}); !errors.Is(e, ErrUnverified) {
			t.Fatal("unverified metadata enabled DDL")
		}
		return
	}
	if len(p.Metadata["tables"]) != 72 || len(p.Metadata["columns"]) != 606 || len(p.Metadata["checks"]) != 117 || len(p.Metadata["indexes"]) != 269 || len(p.Metadata["foreign_keys"]) != 51 || len(p.Metadata["triggers"]) != 22 {
		t.Fatal("verified metadata coverage differs")
	}
	if e := CheckFull(p.Metadata); e != nil {
		t.Fatal(e)
	}
	empty := Snapshot{}
	for section := range p.Metadata {
		empty[section] = [][]*string{}
	}
	actions, e := Preflight(empty)
	if e != nil || len(actions) != 217 {
		t.Fatal("empty schema plan differs", e)
	}
}
