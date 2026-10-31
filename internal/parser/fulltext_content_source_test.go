//go:build legacyinterop

package parser

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"testing"
)

func TestUnchangedFulltextContentSource(t *testing.T) {
	output, err := exec.Command("node", "../../scripts/check-fulltext-content.mjs").Output()
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Markdown string
		Status         int
		Errors         []string
	}
	if err = json.Unmarshal(output, &cases); err != nil || len(cases) < 16 {
		t.Fatalf("source frames: %v", err)
	}
	for _, frame := range cases {
		t.Run(frame.Name, func(t *testing.T) {
			if got := CheckFulltextContent(frame.Markdown, frame.Status); !reflect.DeepEqual(got, frame.Errors) {
				t.Fatalf("got %v; source %v", got, frame.Errors)
			}
		})
	}
}
