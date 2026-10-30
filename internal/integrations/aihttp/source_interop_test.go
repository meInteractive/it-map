//go:build legacyinterop

package aihttp

import (
	"os/exec"
	"testing"
)

func TestUnchangedProviderTransportSource(t *testing.T) {
	// Выполняются actual source exports, глобальный fetch полностью заменён mocks.
	out, err := exec.Command("node", "../../../scripts/check-ai-provider-transport.mjs").Output()
	if err != nil {
		t.Fatal("source oracle failed", err)
	}
	sourceCases(t, out)
}
