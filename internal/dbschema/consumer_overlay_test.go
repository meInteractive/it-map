package dbschema

import (
	"encoding/json"
	"testing"
)

func copyConsumerSnapshot(t *testing.T, source Snapshot) Snapshot {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var result Snapshot
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func completeConsumerSnapshot(t *testing.T) Snapshot {
	t.Helper()
	baseline := copyConsumerSnapshot(t, Trusted().Metadata)
	for section, rows := range ConsumerOverlayMetadata() {
		baseline[section] = append(baseline[section], rows...)
	}
	return baseline
}

func TestConsumerOverlayRequiresExactBaselineAndControlMetadata(t *testing.T) {
	snapshot := completeConsumerSnapshot(t)
	if err := CheckConsumerOverlay(snapshot, ConsumerOverlayVersion); err != nil {
		t.Fatal("complete overlay rejected", err)
	}
	if err := CheckConsumerOverlay(snapshot, 0); err == nil {
		t.Fatal("unknown version accepted")
	}
	cases := []struct {
		name   string
		change func(Snapshot)
	}{
		{"missing control", func(s Snapshot) { s["tables"] = s["tables"][:len(s["tables"])-1] }},
		{"duplicate control", func(s Snapshot) { r := s["tables"][len(s["tables"])-1]; s["tables"] = append(s["tables"], r) }},
		{"changed control column type", func(s Snapshot) { rows := s["columns"]; r := rows[len(rows)-1]; v := "bigint unsigned"; r[3] = &v }},
		{"nullable generation", func(s Snapshot) { rows := s["columns"]; r := rows[len(rows)-5]; v := "YES"; r[4] = &v }},
		{"extra control index", func(s Snapshot) {
			rows := s["indexes"]
			r := append([]*string(nil), rows[len(rows)-1]...)
			v := "EXTRA"
			r[1] = &v
			s["indexes"] = append(rows, r)
		}},
		{"missing baseline column", func(s Snapshot) { s["columns"] = s["columns"][1:] }},
		{"extra unknown table", func(s Snapshot) { v := "unknown"; s["tables"] = append(s["tables"], []*string{&v, nil, nil, nil}) }},
		{"missing section", func(s Snapshot) { delete(s, "triggers") }},
		{"unexpected section", func(s Snapshot) { s["unknown"] = nil }},
		{"invalid row", func(s Snapshot) { s["checks"] = append(s["checks"], []*string{nil}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := copyConsumerSnapshot(t, snapshot)
			tc.change(s)
			if err := CheckConsumerOverlay(s, ConsumerOverlayVersion); err == nil {
				t.Fatal("incompatible metadata accepted")
			}
		})
	}
}

func TestConsumerOverlayMetadataIsIndependent(t *testing.T) {
	first := ConsumerOverlayMetadata()
	v := "changed"
	first["columns"][0][1] = &v
	if *ConsumerOverlayMetadata()["columns"][0][1] != "id" {
		t.Fatal("trusted metadata mutated")
	}
}
