//go:build legacyinterop

package httpapi

import (
	"encoding/json"
	"io"
	"itmap/internal/backup"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestSchedulerQueueFiltersSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-scheduler-queue-read.mjs")
	cmd.Stderr = os.Stderr
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if cmd.Start() != nil {
		t.Fatal("source queue oracle не запущен")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	d, count := json.NewDecoder(pipe), 0
	for {
		var v struct {
			Name, User, Method, Query string
			NoDB                      bool
			QueryObject               json.RawMessage
			Status                    int
			OwnerMatches              []struct {
				Name    string
				Matched bool
			}
		}
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		count++
		var got, want any
		if json.Unmarshal(schedulerQueryObject(v.Query), &got) != nil || json.Unmarshal(v.QueryObject, &want) != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("URLSearchParams/Object.fromEntries decoding отличается", v.Name)
		}
		if v.Method != "GET" || v.User != "Eugene" || v.NoDB {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			f, e := backup.PrepareSchedulerQueueFilters(v.QueryObject)
			if (e == nil) != (v.Status == 200) {
				t.Fatalf("source query admission отличается: %v/%d", e, v.Status)
			}
			if e == nil {
				for _, o := range v.OwnerMatches {
					if f.MatchOwner(o.Name) != o.Matched {
						t.Fatal("source UTF16 trim/lower/includes отличается")
					}
				}
			}
		})
	}
	if cmd.Wait() != nil || count != 106 {
		t.Fatal("неполный source queue corpus", count)
	}
}
