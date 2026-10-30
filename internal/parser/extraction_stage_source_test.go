//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedExtractionStageSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-extraction-stage-contract.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionStageSource(t, b)
}
