//go:build legacyinterop

package backup

import (
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyV39RestorePlan(t *testing.T) {
	cmd := exec.Command("node", filepath.Join("..", "..", "scripts", "check-v39-restore-plan.mjs"), "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if e = cmd.Start(); e != nil {
		t.Fatal("cannot start required restore oracle")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(pipe)
	count := 0
	for {
		var v struct {
			Name, Body string
			OK         bool
			Expected   struct {
				Tables []struct {
					Name string
					Rows []map[string]json.RawMessage
				}
				InsertOrder, DeleteOrder, Warnings []string
				Rows                               int
				Sequences                          []Sequence
				Catalog                            json.RawMessage
			}
		}
		if e = decoder.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal("invalid restore oracle output")
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			plan, e := PrepareV39Restore([]byte(v.Body))
			if (e == nil) != v.OK {
				t.Fatalf("legacy restore plan decision differs: %v", e)
			}
			if !v.OK {
				return
			}
			if !reflect.DeepEqual(plan.InsertOrder, v.Expected.InsertOrder) || !reflect.DeepEqual(plan.DeleteOrder, v.Expected.DeleteOrder) || !reflect.DeepEqual(plan.Warnings, v.Expected.Warnings) || plan.Rows != v.Expected.Rows || len(plan.Tables) != len(v.Expected.Tables) {
				t.Fatal("legacy restore ordering/warnings/counts differ")
			}
			if !reflect.DeepEqual(plan.Sequences, v.Expected.Sequences) || string(plan.Catalog) != string(v.Expected.Catalog) {
				t.Fatal("restore plan changed catalog or input counters")
			}
			for i, table := range plan.Tables {
				if table.Name != v.Expected.Tables[i].Name || !reflect.DeepEqual(table.Rows, v.Expected.Tables[i].Rows) {
					t.Fatalf("legacy restored values differ in %s", table.Name)
				}
			}
			if _, e = ValidateV39([]byte(v.Body)); e != nil {
				t.Fatal("planning changed original signed input")
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatalf("required source restore oracle failed: %s", stderr.String())
	}
	if count != 28 {
		t.Fatalf("missing restore vectors: %d", count)
	}
}
