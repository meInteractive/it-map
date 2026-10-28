//go:build legacyinterop

package vacancies

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPublicationPeriodSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-publication-period.mjs")
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
			Kind, Raw   string
			Valid, Want bool
			Time, Now   int64
			Period      Period
			Dates       Dates
		}
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		count++
		match := false
		switch v.Kind {
		case "date":
			d, ok := PublicationDate(v.Raw)
			match = ok == v.Valid && (!ok || d.UnixMilli() == v.Time)
		case "match":
			match = MatchesPublication(v.Period, v.Dates, time.UnixMilli(v.Now)) == v.Want
		default:
			t.Fatal("unreviewed publication source fixture")
		}
		if !match {
			mismatches++
			if mismatches <= 6 {
				t.Errorf("source publication mismatch: %+v", v)
			}
		}
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal("required unchanged publication oracle failed", e)
	}
	if count != 27375 {
		t.Fatal("missing publication corpus", count)
	}
	if mismatches != 0 {
		t.Fatal("publication source mismatches", mismatches)
	}
	t.Logf("PASS: %d unchanged source date/filter frames", count)
}
