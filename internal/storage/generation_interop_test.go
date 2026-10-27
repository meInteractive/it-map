//go:build legacyinterop

package storage

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLegacyGenerationBoundary(t *testing.T) {
	out, e := exec.Command("node", "../../scripts/check-generation-restore.mjs").CombinedOutput()
	if e != nil || strings.TrimSpace(string(out)) != "PASS: 4 unchanged legacy generation transaction cases; memory only" {
		t.Fatal("unchanged legacy generation transaction oracle failed")
	}
}
