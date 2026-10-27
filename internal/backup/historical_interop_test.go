//go:build legacyinterop

package backup

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

func TestLegacyHistoricalFormat(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-historical-format.mjs", "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal("required historical source oracle unavailable")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(bufio.NewReader(pipe))
	count := 0
	versions := map[int]bool{}
	for {
		var v struct {
			Name, Body      string
			OK              bool
			Version, Tables int
		}
		if e = decoder.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal("invalid historical oracle output")
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			p, e := ValidateModernBackup([]byte(v.Body))
			if (e == nil) != v.OK {
				t.Fatalf("modern validation differs: source=%v Go error=%v", v.OK, e)
			}
			if v.OK {
				versions[v.Version] = true
				if p.Version != float64(v.Version) || len(p.Storage.Tables) != v.Tables {
					t.Fatal("version or table projection differs")
				}
			}
			_, currentErr := ValidateV39([]byte(v.Body))
			if (currentErr == nil) != (v.OK && v.Version == 39) {
				t.Fatal("v39-only guard widened")
			}
			if v.Version != 39 && currentErr.Error() != "Поддерживается формат бэкапа v39." {
				t.Fatal("current HTTP version error changed")
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("source oracle failed")
	}
	if count != 416 || len(versions) != 24 {
		t.Fatalf("missing historical coverage: %d vectors %d versions", count, len(versions))
	}
}
