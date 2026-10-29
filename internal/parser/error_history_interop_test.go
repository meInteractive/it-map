//go:build legacyinterop

package parser

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

func TestErrorHistoryCalendarSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-parser-error-history.mjs")
	cmd.Stderr = os.Stderr
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if cmd.Start() != nil {
		t.Fatal("source history oracle failed")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var v struct {
			Name      string
			Now       int64
			ReadError string
			Body      *struct{ Points []struct{ Day string } }
			Records   []struct {
				Owner     string
				CountsRaw string
				Now       int64
			}
			Errors      []string
			StorageRows []map[string]string
		}
		e = decoder.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			if v.ReadError == "" {
				if v.Body == nil || len(v.Body.Points) != 30 {
					t.Fatal("source history window missing")
				}
				for i, p := range v.Body.Points {
					day, e := historyDay(v.Now - int64(29-i)*86400000)
					if e != nil || day != p.Day {
						t.Fatal("source Moscow/calendar day differs")
					}
				}
			}
			if len(v.Records) == 1 && v.Errors[0] == "ok" {
				var counts ErrorHistoryCounts
				if json.Unmarshal([]byte(v.Records[0].CountsRaw), &counts) != nil {
					t.Fatal("invalid counts")
				}
				if len(v.StorageRows) != 1 {
					t.Fatal("source stored row missing")
				}
				for key, n := range map[string]int64{"p1_peak": counts.P1, "p2_peak": counts.P2} {
					value, e := recordSourceCount(n)
					if e != nil || strconv.FormatInt(value, 10) != v.StorageRows[0][key] {
						t.Fatal("source Number→INTEGER stored count differs")
					}
				}
			}
		})
	}
	if cmd.Wait() != nil || count != 19 {
		t.Fatal("incomplete source history corpus", count)
	}
}
