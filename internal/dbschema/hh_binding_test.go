package dbschema

import "testing"

func TestHHBindingMetadataRequiresCompleteExactSchema(t *testing.T) {
	snapshot := completeConsumerSnapshot(t)
	for section, rows := range ConsumerVersionMetadata() {
		snapshot[section] = append(snapshot[section], rows...)
	}
	for section, rows := range HHBindingCandidateMetadata() {
		snapshot[section] = append(snapshot[section], rows...)
	}
	if err := CheckHHBindingCandidate(snapshot, HHBindingCandidateVersion); err != nil {
		t.Fatal("valid candidate rejected", err)
	}
	cases := []struct {
		name   string
		change func(Snapshot)
	}{
		{"missing binding", func(s Snapshot) { s["tables"] = s["tables"][:len(s["tables"])-1] }},
		{"unknown column", func(s Snapshot) {
			rows := s["columns"]
			r := append([]*string(nil), rows[len(rows)-1]...)
			v := "ci_extra"
			r[1] = &v
			s["columns"] = append(rows, r)
		}},
		{"changed column", func(s Snapshot) { r := s["columns"][len(s["columns"])-1]; v := "YES"; r[4] = &v }},
		{"missing unique index", func(s Snapshot) { s["indexes"] = s["indexes"][:len(s["indexes"])-1] }},
		{"missing marker", func(s Snapshot) { r := s["tables"]; s["tables"] = append(r[:len(r)-2], r[len(r)-1:]...) }},
		{"missing baseline", func(s Snapshot) { s["columns"] = s["columns"][1:] }},
		{"unknown table", func(s Snapshot) { v := "ci_unknown"; s["tables"] = append(s["tables"], []*string{&v, nil, nil, nil}) }},
		{"missing section", func(s Snapshot) { delete(s, "triggers") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := copyConsumerSnapshot(t, snapshot)
			tc.change(s)
			if CheckHHBindingCandidate(s, HHBindingCandidateVersion) == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	if CheckHHBindingCandidate(snapshot, 2) == nil {
		t.Fatal("unknown candidate version accepted")
	}
	first := HHBindingCandidateMetadata()
	v := "modified"
	first["columns"][0][1] = &v
	if *HHBindingCandidateMetadata()["columns"][0][1] != "operation_id" {
		t.Fatal("trusted metadata mutated")
	}
}
