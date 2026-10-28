//go:build legacyinterop

package parser

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"

	"itmap/internal/backup"
)

func TestStoredSchedulerSettingsSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-stored-scheduler.mjs")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	dec := json.NewDecoder(pipe)
	count, mismatches := 0, 0
	for {
		var v struct {
			Kind, Name, Raw, Body, Stored, Public, Message, OwnerName string
			Favorites, Error                                          bool
			LegacySources                                             []string
			Input                                                     json.RawMessage
		}
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		count++
		view, e := backup.ReadSchedulerSettingsJSON([]byte(v.Raw), v.Favorites, v.LegacySources)
		match := false
		if v.Kind == "read" {
			match = (e != nil) == v.Error && (e != nil || string(view.PublicJSON()) == v.Body)
			if e == nil {
				safe, err := json.Marshal(view)
				normalized, parseErr := backup.StringifyParsedJSON(safe)
				match = match && err == nil && parseErr == nil && string(normalized) == v.Body
			}
		} else {
			if e != nil {
				t.Fatal("save fixture has invalid source read")
			}
			var previous Settings
			if e = json.Unmarshal(view.PublicJSON(), &previous); e != nil {
				t.Fatal("unreviewed save state type", v.Name)
			}
			_, e = ValidateSettingsUpdate(v.Input, previous, v.OwnerName)
			if v.Error {
				match = e != nil && e.Error() == v.Message
			} else if e == nil {
				change, err := view.NextFromValidated(v.Input, v.OwnerName)
				safe, marshalErr := json.Marshal(change)
				normalized, parseErr := backup.StringifyParsedJSON(safe)
				match = err == nil && marshalErr == nil && string(change.StorageJSON()) == v.Stored && string(change.PublicJSON()) == v.Public && parseErr == nil && string(normalized) == v.Public
			}
		}
		if !match {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("stored settings mismatch %s/%s error=%v", v.Kind, v.Name, e)
			}
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("unchanged stored scheduler source failed", e)
	}
	if count != 2214 {
		t.Fatal("missing stored settings corpus", count)
	}
	if mismatches != 0 {
		t.Fatal("stored settings mismatches", mismatches)
	}
	t.Logf("PASS: %d stored scheduler source frames", count)
}
