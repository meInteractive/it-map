//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedCandidateStageSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-candidate-stage-plan.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkCandidateStageSource(t, b)
}
