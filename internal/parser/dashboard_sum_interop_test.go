//go:build legacyinterop

package parser

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestSchedulerDashboardSumSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-dashboard-sum.mjs")
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
		var v struct {
			Values       []string
			Body, Status string
		}
		e = decoder.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		var sum dashboardSum
		var failure error
		for _, raw := range v.Values {
			if failure = sum.addJSON([]byte(raw)); failure != nil {
				break
			}
		}
		var body json.RawMessage
		if failure == nil {
			body, failure = sum.coalescedJSON()
		}
		if v.Status == "storage" {
			if failure == nil {
				t.Fatalf("SQLite SUM не отказал на наборе%d", count)
			}
		} else if failure != nil || string(body) != v.Body {
			t.Fatalf("Сумма отличается от SQLite на наборе%d: %s/%s", count, body, v.Body)
		}
		count++
	}
	if e = cmd.Wait(); e != nil || count != 260 {
		t.Fatal("Неполный эталон SQLite SUM", count)
	}
}
