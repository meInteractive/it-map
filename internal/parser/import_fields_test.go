package parser

import "testing"

func TestImportFieldOwnership(t *testing.T) {
	cases := []struct {
		name              string
		current, incoming any
		existing, manual  bool
		ledger            string
		want              any
		managed, changed  bool
	}{
		{"new", nil, "new", false, false, "", "new", true, true},
		{"owned", "old", "new", true, false, `"old"`, "new", true, true},
		{"manual", "old", "new", true, true, `"old"`, "old", false, false},
		{"edited without ledger update", "human", "new", true, false, `"old"`, "human", false, false},
		{"unknown ownership", "old", "new", true, false, "", "old", false, false},
		{"empty existing", "", "new", true, false, "", "new", true, true},
		{"empty incoming", "old", nil, true, false, `"old"`, "old", true, false},
		{"unchanged manual", "old", "old", true, true, `"old"`, "old", false, false},
		{"salary zero", int64(0), int64(2), true, false, `0`, int64(2), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planImportField(tc.current, tc.incoming, tc.existing, tc.manual, tc.ledger)
			if got.Value != tc.want || got.Managed != tc.managed || got.Changed != tc.changed {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestImportSalaryConflictPreservesBoth(t *testing.T) {
	a := planImportField(int64(100), int64(300), true, false, `100`)
	b := planImportField(int64(200), nil, true, true, `200`)
	a, b = protectImportSalary(a, b, int64(100), int64(200))
	if a.Value != int64(100) || b.Value != int64(200) || a.Managed || b.Managed || a.Changed || b.Changed {
		t.Fatalf("got %+v %+v", a, b)
	}
}
