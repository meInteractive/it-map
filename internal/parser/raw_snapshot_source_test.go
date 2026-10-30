//go:build legacyinterop

package parser

import (
	"os/exec"
	"testing"
)

func TestUnchangedRabotaSnapshotSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-rabota-snapshot.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkRabotaSnapshotSource(t, b)
}
