//go:build legacyinterop

package backup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"
)

func TestLegacyUnsignedFormat(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-legacy-format.mjs", "--stdio")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal("required legacy source oracle unavailable")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(bufio.NewReader(pipe))
	count, accepted := 0, 0
	versions := map[int]bool{}
	var valid []byte
	for {
		var v struct {
			Name, Body, Error string
			OK, ExactError    bool
			Version, Tables   int
		}
		if e = decoder.Decode(&v); e == io.EOF {
			break
		} else if e != nil {
			t.Fatal("invalid legacy oracle output")
		}
		count++
		t.Run(v.Name, func(t *testing.T) {
			data := []byte(v.Body)
			original := append([]byte(nil), data...)
			p, e := ValidateLegacyBackup(data)
			if (e == nil) != v.OK {
				t.Fatalf("legacy validation differs: source=%v Go error=%v", v.OK, e)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("caller input mutated")
			}
			if v.OK {
				accepted++
				versions[v.Version] = true
				if valid == nil {
					valid = data
				}
				if p.Version != float64(v.Version) || len(p.Storage.Tables) != v.Tables {
					t.Fatal("legacy version or table projection differs")
				}
				// Exact raw scalar/UTF16 projection, including explicit NULL and
				// canonical numeric identity, is compared to the parsed source.
				root, _ := orderedJSON(data)
				if !bytes.Equal(p.Catalog, rawValue(root.field("catalog"))) || !bytes.Equal(p.CreatedAt, rawValue(root.field("createdAt"))) {
					t.Fatal("legacy catalog/date changed during validation")
				}
				for i, node := range root.field("storage").field("tables").items {
					got := p.Storage.Tables[i]
					if got.Name != textField(node, "name") {
						t.Fatal("source table order changed")
					}
					for j, column := range node.field("columns").items {
						if !bytes.Equal(got.Columns[j], rawValue(column)) {
							t.Fatal("source column descriptor changed")
						}
					}
					for j, row := range node.field("rows").items {
						for _, m := range row.members {
							if !bytes.Equal(got.Rows[j][stringFromUnits(m.key)], rawValue(m.value)) {
								t.Fatal("source scalar changed")
							}
						}
					}
				}
				if p.Metadata.Sequences != nil || p.Metadata.Schema != nil || p.Metadata.Migrations != nil || p.Manifest != nil || p.Integrity.Algorithm != "" || p.Integrity.Digest != "" {
					t.Fatal("unsigned legacy metadata promoted to trusted plan fields")
				}
			} else {
				if ErrorStatus(e) != 400 {
					t.Fatal("invalid legacy input not a safe 400")
				}
				// The dedicated entry point reports its own supported range.
				if v.ExactError && e.Error() != "Поддерживаются legacy-бэкапы v14–15." && e.Error() != v.Error {
					t.Fatalf("source diagnostic differs: source=%q Go=%q", v.Error, e.Error())
				}
			}
			if _, e := ValidateModernBackup(data); e == nil {
				t.Fatal("signed modern guard widened")
			}
			if _, e := ValidateV39(data); e == nil {
				t.Fatal("strict v39 guard widened")
			}
		})
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required legacy source oracle failed")
	}
	if count != 1468 || accepted != 469 || len(versions) != 2 {
		t.Fatalf("missing legacy coverage: %d vectors, %d accepted, %d versions", count, accepted, len(versions))
	}
	// Modern-specific planning stays strict. Generic runtime now accepts legacy
	// plans and reaches the storage boundary, whose actual write gate runs in CI.
	if _, e := PrepareModernRestore(valid, nil, time.Now()); e == nil || ErrorStatus(e) != 400 {
		t.Fatal("modern plan accepted unsigned legacy data")
	}
	if _, e := (Repository{}).ValidateRestore(context.Background(), valid); e == nil || ErrorStatus(e) == 400 {
		t.Fatal("runtime validation did not reach storage for valid legacy input")
	}
	if _, e := (Repository{}).Restore(context.Background(), valid); e == nil || ErrorStatus(e) == 400 {
		t.Fatal("runtime restore did not reach storage for valid legacy input")
	}
	// Request-size enforcement is shared with ReadRequest; source full-layout
	// validation itself has no byte cap and cannot serve as an import boundary.
	oversized := append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"ignored":"`)...)
	oversized = append(oversized, bytes.Repeat([]byte{'x'}, MaxBytes)...)
	oversized = append(oversized, []byte(`"}`)...)
	if _, e := ValidateLegacyBackup(oversized); ErrorStatus(e) != 413 {
		t.Fatal("legacy direct byte cap missing")
	}
}
