//go:build legacyinterop

package contacts

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"reflect"
	"testing"
)

func TestLegacyContactInput(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-contact-input.mjs", "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal("required source contact oracle unavailable")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 100000)
	count := 0
	for scanner.Scan() {
		var v struct {
			Name           string
			Input          Input
			CurrentCompany string
			Expected       *NormalizedInput
			Error          string
		}
		if json.Unmarshal(scanner.Bytes(), &v) != nil {
			t.Fatal("invalid source contact vector")
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			got, e := NormalizeInput(v.Input, v.CurrentCompany, nil)
			if v.Expected == nil {
				if e == nil || e.Error() != v.Error {
					t.Fatalf("source error differs: Go %v, JS %s", e, v.Error)
				}
			} else if e != nil || !reflect.DeepEqual(got, *v.Expected) {
				t.Fatalf("source input differs: Go %+v (%v), JS %+v", got, e, *v.Expected)
			}
		})
	}
	if scanner.Err() != nil || cmd.Wait() != nil || count != 297 {
		t.Fatal("incomplete required source contact coverage")
	}
}
