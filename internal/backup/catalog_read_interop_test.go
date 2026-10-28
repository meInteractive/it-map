//go:build legacyinterop

package backup

import (
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

func TestCatalogReadProjectionSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-catalog-read.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count, mismatches := 0, 0
	for {
		var v struct {
			Name, Raw, Body string
			State           CatalogReadState
			Options         CatalogReadOptions
			Error           bool
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal(e)
		}
		count++
		actual, e := PrepareCatalogRead([]byte(v.Raw), v.State, v.Options)
		if (e != nil) != v.Error || e == nil && string(actual) != v.Body {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("source projection %s differs: error %v expectedError %t\nGo: %s\nsource: %s", v.Name, e, v.Error, actual, v.Body)
			}
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required projection oracle failed", e)
	}
	if count != 1180 {
		t.Fatal("missing projection fixtures", count)
	}
	if mismatches > 0 {
		t.Fatal("projection mismatches", mismatches)
	}
}
