//go:build legacyinterop

package backup

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestSchedulerAttemptProjectionSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-scheduler-attempt-projection.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var v struct{ Input, Body string }
		e = decoder.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		body, e := PrepareSchedulerAttemptJSON([]byte(v.Input))
		if e != nil || string(body) != v.Body {
			t.Fatalf("source attempt projection differs on frame %d", count)
		}
		count++
	}
	if e = cmd.Wait(); e != nil || count != 368 {
		t.Fatal("required original attempt projection corpus incomplete", count)
	}
	t.Logf("PASS: %d source bounded attempt projections", count)
}
