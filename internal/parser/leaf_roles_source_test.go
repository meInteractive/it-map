//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedLeafRolesSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-leaf-roles-contract.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkLeafRolesSource(t, b)
}
