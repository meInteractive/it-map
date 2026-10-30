package parser

import (
	"reflect"
	"testing"
)

func TestFulltextContentBasics(t *testing.T) {
	if got := CheckFulltextContent("", 404); !reflect.DeepEqual(got, []string{fulltextShort, fulltextStatus, fulltextSignals}) {
		t.Fatalf("unexpected errors: %v", got)
	}
}
