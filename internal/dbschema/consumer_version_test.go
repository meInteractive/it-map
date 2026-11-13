package dbschema

import "testing"

func TestVersionedConsumerMetadataRequiresAllExactObjects(t *testing.T) {
	snapshot := completeConsumerSnapshot(t)
	for section, rows := range ConsumerVersionMetadata() {
		snapshot[section] = append(snapshot[section], rows...)
	}
	if err := CheckVersionedConsumerOverlay(snapshot, ConsumerSchemaVersion); err != nil {
		t.Fatal("valid versioned candidate rejected", err)
	}
	tests := []struct {
		name   string
		change func(Snapshot)
	}{
		{"missing marker table", func(s Snapshot) { s["tables"] = s["tables"][:len(s["tables"])-1] }},
		{"extra marker column", func(s Snapshot) {
			rows := s["columns"]
			r := append([]*string(nil), rows[len(rows)-1]...)
			v := "ci_extra"
			r[1] = &v
			s["columns"] = append(rows, r)
		}},
		{"wrong marker type", func(s Snapshot) { r := s["columns"][len(s["columns"])-1]; v := "varchar(20)"; r[3] = &v }},
		{"extra marker index", func(s Snapshot) {
			r := append([]*string(nil), s["indexes"][len(s["indexes"])-1]...)
			v := "extra"
			r[1] = &v
			s["indexes"] = append(s["indexes"], r)
		}},
		{"missing consumer", func(s Snapshot) { rows := s["tables"]; s["tables"] = append(rows[:len(rows)-2], rows[len(rows)-1:]...) }},
		{"missing baseline", func(s Snapshot) { s["columns"] = s["columns"][1:] }},
		{"unknown table", func(s Snapshot) { v := "ci_unknown"; s["tables"] = append(s["tables"], []*string{&v, nil, nil, nil}) }},
		{"missing metadata section", func(s Snapshot) { delete(s, "triggers") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := copyConsumerSnapshot(t, snapshot)
			tc.change(s)
			if CheckVersionedConsumerOverlay(s, ConsumerSchemaVersion) == nil {
				t.Fatal("invalid versioned metadata accepted")
			}
		})
	}
	if CheckVersionedConsumerOverlay(snapshot, 2) == nil {
		t.Fatal("unsupported version accepted")
	}
	first := ConsumerVersionMetadata()
	v := "changed"
	first["columns"][0][1] = &v
	if *ConsumerVersionMetadata()["columns"][0][1] != "id" {
		t.Fatal("trusted metadata mutated")
	}
}
