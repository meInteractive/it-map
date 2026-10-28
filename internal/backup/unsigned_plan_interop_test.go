//go:build legacyinterop

package backup

import (
	"encoding/json"
	"io"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestLegacyUnsignedPlan(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-unsigned-plan.mjs", "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal("historical source planner unavailable")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count := 0
	for {
		var v struct {
			Name, Body, Now string
			Current         []Table
			OK              bool
			Expected        struct {
				Tables []struct {
					Name    string
					Columns []string
					Rows    []map[string]json.RawMessage
				}
				Warnings, InsertOrder, DeleteOrder []string
				Rows                               int
			}
		}
		if e = dec.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal("invalid historical plan oracle output")
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			now, e := time.Parse(time.RFC3339, v.Now)
			if e != nil {
				t.Fatal(e)
			}
			// MariaDB export encoding escapes U+2028/U+2029 unlike JS stringify.
			// Equivalent scalar spellings must not break stable-key/token joins.
			for _, table := range v.Current {
				for _, row := range table.Rows {
					for k, raw := range row {
						if len(raw) > 0 && raw[0] == '"' {
							var text string
							if json.Unmarshal(raw, &text) == nil {
								row[k] = encodeScalarText(text)
							}
						}
					}
				}
			}
			before, _ := json.Marshal(v.Current)
			body := []byte(v.Body)
			plan, e := PrepareLegacyRestore(body, v.Current, now)
			after, _ := json.Marshal(v.Current)
			if string(before) != string(after) || string(body) != v.Body {
				t.Fatal("planner mutated caller input/state")
			}
			if (e == nil) != v.OK {
				t.Fatalf("historical source decision differs: %v", e)
			}
			if !v.OK {
				return
			}
			if !reflect.DeepEqual(plan.Warnings, v.Expected.Warnings) || !reflect.DeepEqual(plan.InsertOrder, v.Expected.InsertOrder) || !reflect.DeepEqual(plan.DeleteOrder, v.Expected.DeleteOrder) || plan.Rows != v.Expected.Rows || len(plan.Tables) != 52 {
				t.Fatal("historical counts/warnings/order differ")
			}
			for i, got := range plan.Tables {
				want := v.Expected.Tables[i]
				names, e := columnNames(got)
				if e != nil || got.Name != want.Name || !reflect.DeepEqual(names, want.Columns) {
					t.Fatalf("historical table/column ordering differs: %s want %s", got.Name, want.Name)
				}
				if !reflect.DeepEqual(got.Rows, want.Rows) {
					a, _ := json.Marshal(got.Rows)
					b, _ := json.Marshal(want.Rows)
					t.Fatalf("historical rows differ in %s\nGo: %s\nJS: %s", got.Name, a, b)
				}
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("unchanged historical source oracle failed")
	}
	if count != 170 {
		t.Fatalf("missing historical plans: %d", count)
	}
}

// Actual unchanged source SQL executes only on synthetic SQLite :memory:.
// MariaDB acceptance is a separate guarded CI job, never a local database.
func TestLegacyUnsignedSQLPlan(t *testing.T) {
	for _, version := range []string{"14", "15"} {
		for _, audit := range []string{"omitted", "empty", "replace"} {
			t.Run(version+" "+audit, func(t *testing.T) {
				out, e := exec.Command("node", "../../scripts/check-historical-sql.mjs", "--version", version, "--audit", audit, "--stdio").Output()
				if e != nil {
					t.Fatal(e)
				}
				var v struct {
					Initial, Body string
					Expected      []Table
					Warnings      []string
					Rows          int
				}
				if e = json.Unmarshal(out, &v); e != nil {
					t.Fatal(e)
				}
				initial, e := ValidateV39([]byte(v.Initial))
				if e != nil {
					t.Fatal(e)
				}
				plan, e := PrepareLegacyRestore([]byte(v.Body), initial.Storage.Tables, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))
				if e != nil {
					t.Fatal(e)
				}
				if plan.Rows != v.Rows || !reflect.DeepEqual(plan.Warnings, v.Warnings) || len(plan.Sequences) != 0 {
					t.Fatal("unsigned SQL source summary differs")
				}
				for _, want := range v.Expected {
					for _, got := range plan.Tables {
						if want.Name == got.Name && !reflect.DeepEqual(got.Rows, want.Rows) {
							t.Fatalf("source SQL rows differ in %s", got.Name)
						}
					}
				}
			})
		}
	}
}
