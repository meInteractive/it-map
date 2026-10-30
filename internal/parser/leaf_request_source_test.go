//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedLeafRequestSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-leaf-role-request.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkLeafRequestSource(t, b)
}

func TestUnchangedLeafStageSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-leaf-role-request.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkLeafStageHashes(t, b)
}
