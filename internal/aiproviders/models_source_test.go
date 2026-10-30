//go:build legacyinterop

package aiproviders

import (
	"os/exec"
	"testing"
)

func TestUnchangedModelCatalogSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-ai-model-catalog.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkCatalogSource(t, b)
}
