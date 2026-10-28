//go:build legacyinterop

package parser

import (
	"encoding/json"
	"io"
	"math"
	"os/exec"
	"testing"
)

func TestSchedulerBillingSource(t *testing.T) {
	cmd := exec.Command("node", "scripts/check-scheduler-billing.mjs")
	cmd.Dir = "../.."
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(pipe)
	count := 0
	for {
		var v struct {
			Name, Throw, ResponseRaw string
			Request, Response        json.RawMessage
			Upper                    float64
			Unknown                  bool
			Executions               int
			Attempt                  struct {
				Reserved float64 `json:"reserved_usd"`
			}
		}
		if e = decoder.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal(e)
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			upper, e := UpperCostJSON(v.Request)
			if e != nil || math.Abs(upper-v.Upper) > 1e-12 {
				t.Fatal("upper cost differs from source", e)
			}
			if v.Executions == 0 || v.Throw != "" {
				return
			}
			response := v.Response
			if v.ResponseRaw != "" {
				response = []byte(v.ResponseRaw)
			}
			cost, known := actualCost(v.Request, response)
			if known == v.Unknown || known && math.Abs(cost-v.Attempt.Reserved) > 1e-12 {
				t.Fatal("actual cost/unknown precedence differs", cost, known, v.Attempt.Reserved, v.Unknown)
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("source billing oracle failed", e)
	}
	if count != 38 {
		t.Fatal("missing billing source cases", count)
	}
}
