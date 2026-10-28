//go:build legacyinterop

package parser

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestSettingsRevisionCASSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-settings-cas.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count := 0
	for {
		var v struct {
			Raw      string
			Expected int64
			Match    bool
		}
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		got, e := settingsRevisionMatches([]byte(v.Raw), v.Expected)
		if e != nil || got != v.Match {
			t.Fatalf("source JSON1 CAS differs on %s, safe binding %d", v.Raw, v.Expected)
		}
		count++
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required JSON1 source failed", e)
	}
	if count != 2324 {
		t.Fatal("missing JSON1 CAS comparisons", count)
	}
	t.Logf("PASS: %d JSON1 CAS comparisons", count)
}
