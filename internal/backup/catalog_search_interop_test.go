//go:build legacyinterop

package backup

import (
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

func TestCatalogSearchSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-catalog-search.mjs")
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
			Name, Raw, Body, QueryJSON string
			Error                      bool
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal(e)
		}
		count++
		body, e := PrepareCatalogSearch([]byte(v.Raw), json.RawMessage(v.QueryJSON))
		if (e != nil) != v.Error || e == nil && string(body) != v.Body {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("search %s differs: error %v expectedError %t\nGo: %s\nsource: %s", v.Name, e, v.Error, body, v.Body)
			}
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required source search failed", e)
	}
	if count != 2551 {
		t.Fatal("missing search fixtures", count)
	}
	if mismatches != 0 {
		t.Fatal("search mismatches", mismatches)
	}
}
