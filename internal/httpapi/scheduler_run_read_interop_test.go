//go:build legacyinterop

package httpapi

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestSchedulerRunQuerySource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-scheduler-run-read.mjs")
	cmd.Stderr = os.Stderr
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if cmd.Start() != nil {
		t.Fatal("source run oracle failed")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	d, count := json.NewDecoder(pipe), 0
	for {
		var v struct {
			Query          string
			QueryObject    map[string]any
			FirstRequestID *string
		}
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		expected := v.QueryObject
		if _, ok := expected["requestId"]; ok {
			expected["requestId"] = *v.FirstRequestID
		}
		var actual map[string]any
		if json.Unmarshal(schedulerRunQuery(v.Query), &actual) != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("source query differs: %q got=%v want=%v", v.Query, actual, expected)
		}
		count++
	}
	if cmd.Wait() != nil || count != 32 {
		t.Fatal("missing source run query frames", count)
	}
}
