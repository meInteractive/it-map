//go:build legacyinterop

package aiproviders

import (
	"os/exec"
	"testing"
)

func TestUnchangedJEVCatalogSource(t *testing.T) {
	b, e := exec.Command("node", "../../scripts/check-jev-catalog.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	checkJEVCatalogSource(t, b)
}
