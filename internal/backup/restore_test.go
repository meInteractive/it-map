package backup

import (
	"encoding/json"
	"itmap/internal/v39"
	"testing"
)

func TestRestoreLosslessScalarBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, raw, kind string
		nullable, ok    bool
	}{
		{"Unicode whitespace and NUL", `"  😀 \u0000  "`, "TEXT", false, true},
		{"literal surrogate escape", `"\\ud800"`, "TEXT", false, true},
		{"lone high surrogate", `"\ud800"`, "TEXT", false, false},
		{"lone low surrogate", `"\udfff"`, "TEXT", false, false},
		{"safe negative integer", `-9007199254740991`, "INTEGER", false, true},
		{"unsafe integer", `9007199254740992`, "INTEGER", false, false},
		{"fractional integer", `1.25`, "INTEGER", false, false},
		{"numeric text must not coerce", `"7"`, "INTEGER", false, false},
		{"integer text must not coerce", `7`, "TEXT", false, false},
		{"real", `1.25`, "REAL", false, true},
		{"SQL NULL", `null`, "TEXT", true, true},
		{"required NULL", `null`, "TEXT", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := restoreScalar(json.RawMessage(tc.raw), v39.Column{Type: tc.kind, NotNull: !tc.nullable})
			if (e == nil) != tc.ok {
				t.Fatalf("lossless scalar accepted=%v want=%v", e == nil, tc.ok)
			}
		})
	}
}
func TestRestoreRowComparisonPreservesScalarBoundaries(t *testing.T) {
	d := v39.Table{Columns: []v39.Column{{Name: "a"}, {Name: "b"}}}
	key := func(a, b string) string {
		k, e := restoreRowKey(map[string]json.RawMessage{"a": json.RawMessage(a), "b": json.RawMessage(b)}, d)
		if e != nil {
			t.Fatal(e)
		}
		return k
	}
	if key(`"a,b"`, `"c"`) == key(`"a"`, `"b,c"`) || key(`null`, `0`) == key(`"null"`, `0`) {
		t.Fatal("different stored rows compare equal")
	}
	if key(`"\ud83d\ude00"`, `1e0`) != key(`"😀"`, `1`) {
		t.Fatal("equal Unicode/numeric scalar representations differ")
	}
}

func TestRestoreCatalogCountsMatchLooseSourceLength(t *testing.T) {
	for _, tc := range []struct{ agencies, want string }{
		{`[]`, `0`}, {`[{},{}]`, `2`}, {`"😀"`, `2`}, {`{"length":7}`, `7`}, {`{"length":"CI"}`, `"CI"`}, {`{"length":false}`, `0`}, {`{"length":{}}`, `{}`}, {`7`, `0`}, {`null`, `0`},
	} {
		companies, n := RestoreCatalogCounts([]byte(`{"companies":[{"name":"CI"}],"agencies":` + tc.agencies + `}`))
		if companies != 1 || string(n) != tc.want {
			t.Fatalf("source length for %s: %d %s", tc.agencies, companies, n)
		}
	}
}
