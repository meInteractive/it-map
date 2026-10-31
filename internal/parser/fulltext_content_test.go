package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestFulltextContentBasics(t *testing.T) {
	if got := CheckFulltextContent("", 404); !reflect.DeepEqual(got, []string{fulltextShort, fulltextStatus, fulltextSignals}) {
		t.Fatalf("unexpected errors: %v", got)
	}
}

func TestFulltextUTF16Boundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		units int
		want  string
	}{
		{"😀x", 1, "\ufffd"},
		{"😀x", 2, "😀"},
		{"x😀y", 2, "x\ufffd"},
		{"x😀y", 3, "x😀"},
		{"\xffx", 1, "\ufffd"},
		{"hello", 9, "hello"},
	} {
		if got := fulltextPrefix(tc.input, tc.units); got != tc.want {
			t.Errorf("prefix %q / %d: got %q, want %q", tc.input, tc.units, got, tc.want)
		}
	}
	if fulltextAtLeastUTF16(strings.Repeat("😀", 124)+"x", 250) {
		t.Fatal("249 UTF-16 units reached the minimum")
	}
	if !fulltextAtLeastUTF16(strings.Repeat("😀", 125), 250) {
		t.Fatal("250 UTF-16 units missed the minimum")
	}
}
