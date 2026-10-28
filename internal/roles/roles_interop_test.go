//go:build legacyinterop

package roles

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestRoleCatalogSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-role-catalog.mjs")
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
	count, mismatches := 0, 0
	for {
		var v struct {
			Name string
			Rows []Role
			Body Catalog
		}
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		count++
		got, e := Build(v.Rows)
		if e != nil || !reflect.DeepEqual(got, v.Body) {
			mismatches++
			if mismatches <= 3 {
				t.Errorf("role catalog %s differs from unchanged source (error %v)", v.Name, e)
			}
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required unchanged role source failed", e)
	}
	if count != 164 {
		t.Fatal("missing role source frames", count)
	}
	if mismatches != 0 {
		t.Fatal("role source mismatches", mismatches)
	}
	t.Logf("PASS: %d unchanged role read-set frames", count)
}
