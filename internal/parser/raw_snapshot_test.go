package parser

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func checkRabotaSnapshotSource(t *testing.T, raw []byte) {
	t.Helper()
	var rows []struct {
		Name, Markdown string
		Expected       bool
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) < 16 {
		t.Fatal("source fixture")
	}
	for _, r := range rows {
		t.Run(r.Name, func(t *testing.T) {
			if actual := RabotaBodyPresent(r.Markdown); actual != r.Expected {
				t.Fatal("source body check differs", actual, r.Expected)
			}
		})
	}
}
func TestRabotaSnapshotSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/rabota-body-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkRabotaSnapshotSource(t, b)
}
func TestAssembleRawSnapshotIntegrity(t *testing.T) {
	raw := "Начало 😀 конец"
	good := []RawSnapshotPart{{0, "Начало 😀 "}, {1, "конец"}}
	if got, e := AssembleRawSnapshot(nil, good, stageHash(raw)); e != nil || got != raw {
		t.Fatal("parts not assembled", e)
	}
	if got, e := AssembleRawSnapshot(&raw, nil, stageHash(raw)); e != nil || got != raw {
		t.Fatal("inline not preserved", e)
	}
	for _, parts := range [][]RawSnapshotPart{nil, {{1, "Начало 😀 "}, {0, "конец"}}, {{0, "Начало 😀 "}, {2, "конец"}}, {{0, "changed"}}} {
		if _, e := AssembleRawSnapshot(nil, parts, stageHash(raw)); !errors.Is(e, ErrSnapshotInvalid) {
			t.Fatal("incomplete or changed snapshot accepted")
		}
	}
}
