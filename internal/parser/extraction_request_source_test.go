//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedExtractionRequestSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-extraction-request.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionRequestSource(t, b)
}
