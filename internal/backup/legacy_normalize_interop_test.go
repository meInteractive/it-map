//go:build legacyinterop

package backup

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

func TestLegacyCatalogEntities(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-legacy-catalog.mjs", "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal("required catalog source oracle unavailable")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count, accepted := 0, 0
	kinds := map[string]int{}
	for {
		var v struct {
			Name, Kind, Body string
			OK               bool
			Expected         *string
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal("invalid catalog oracle output")
		}
		count++
		kinds[v.Kind]++
		t.Run(v.Name, func(t *testing.T) {
			input, e := orderedJSON([]byte(v.Body))
			if e != nil {
				t.Fatal(e)
			}
			before := rawValue(input)
			var got *jsonValue
			if v.Kind == "company" {
				got, e = normalizeLegacyCompanyFields(input, 1)
			} else {
				got, e = normalizeLegacyAgencyFields(input)
			}
			if !bytes.Equal(before, rawValue(input)) {
				t.Fatal("entity normalization mutated caller")
			}
			if (e == nil) != v.OK {
				t.Fatalf("source entity decision differs: source=%v Go=%v", v.OK, e)
			}
			if !v.OK {
				if ErrorStatus(e) != 400 {
					t.Fatal("unsafe coercion error")
				}
				return
			}
			accepted++
			want, e := orderedJSON([]byte(*v.Expected))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(rawValue(got), []byte(*v.Expected)) {
				t.Fatalf("source serialized fields differ\nGo: %s\nJS: %s", rawValue(got), rawValue(want))
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required catalog source oracle failed")
	}
	if count != 705 || accepted != 700 || kinds["agency"] != 355 || kinds["company"] != 350 {
		t.Fatalf("catalog coverage incomplete: %d cases/%d accepted/%v", count, accepted, kinds)
	}
}
