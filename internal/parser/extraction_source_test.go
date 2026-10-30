//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedExtractionSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-extraction-validation.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionSource(t, b)
}
