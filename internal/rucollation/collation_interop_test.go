//go:build legacyinterop

package rucollation

import (
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

func TestRussianCollationSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-russian-collation.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count, acceptedDifferences := 0, 0
	for {
		var v struct {
			A, B              []uint16
			Source, Canonical int
			Exception         bool
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal(e)
		}
		count++
		got := Compare(Keys(v.A), Keys(v.B))
		if got != v.Canonical {
			t.Fatalf("pair %d: %04x / %04x: Go=%d canonical=%d source=%d", count, v.A, v.B, got, v.Canonical, v.Source)
		}
		if v.Source != v.Canonical {
			if !v.Exception || v.Canonical != 0 {
				t.Fatal("unapproved source deviation")
			}
			acceptedDifferences++
		} else if v.Exception {
			t.Fatal("unexpected exception marker")
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required Node/ICU oracle failed", e)
	}
	if count != 479072 || acceptedDifferences != 8 {
		t.Fatalf("incomplete oracle: %d pairs, %d deviations", count, acceptedDifferences)
	}
}
