//go:build legacyinterop

package aiproviders

import (
	"os/exec"
	"testing"
)

func TestUnchangedGenerationSource(t *testing.T) {
	out, e := exec.Command("node", "../../scripts/check-ai-generation.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkGenerationSource(t, out)
}

func TestUnchangedRouterGenerationSource(t *testing.T) {
	out, e := exec.Command("node", "../../scripts/check-router-generation.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkRouterGenerationSource(t, out)
}
