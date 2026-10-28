//go:build legacyinterop

package parser

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestSchedulerJSONSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-scheduler-json.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count := 0
	mismatches := 0
	for {
		var v struct {
			Raw, Path, Hex string
			Empty          bool
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal(e)
		}
		count++
		text, present, e := sqliteJSONText([]byte(v.Raw), strings.Split(strings.TrimPrefix(v.Path, "$."), ".")...)
		if e != nil || present == v.Empty || !strings.EqualFold(hex.EncodeToString([]byte(text)), v.Hex) {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("SQLite JSON text differs for %s: Go %q source hex %s error %v", v.Raw, text, v.Hex, e)
			}
		}
	}
	if mismatches > 0 {
		t.Errorf("total JSON mismatches: %d", mismatches)
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required scheduler JSON source oracle failed", e)
	}
	if count != 23044 {
		t.Fatal("missing scheduler JSON vectors", count)
	}
}
