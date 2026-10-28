//go:build legacyinterop

package parser

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"testing"
)

func TestSchedulerClaimSource(t *testing.T) {
	cmd := exec.Command("node", "scripts/check-scheduler-claim.mjs")
	cmd.Dir = "../.."
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(pipe)
	count := 0
	for {
		var v struct {
			Name, Generation, Selected, Error, SettingsRaw string
			Now                                            int64
			Settings                                       Settings
			Jobs                                           []struct {
				Job
				PayloadRaw string
			}
			Options struct {
				Types                         []string
				Diagnostic, DeferredApifyOnly bool
				AllowApify, AllowApifyStarts  *bool
				JobID                         *string
			}
			Pages      map[string]json.RawMessage
			Operations []string
		}
		if err = decoder.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		count++
		jobs := make([]Job, len(v.Jobs))
		for i, j := range v.Jobs {
			jobs[i] = j.Job
			if j.PayloadRaw != "" {
				jobs[i].Payload = []byte(j.PayloadRaw)
			}
		}
		settingsRaw, _ := json.Marshal(v.Settings)
		if v.SettingsRaw != "" {
			settingsRaw = []byte(v.SettingsRaw)
			if e := json.Unmarshal(settingsRaw, &v.Settings); e != nil {
				t.Fatal(e)
			}
		}
		v.Settings.AgenciesEnabled, err = claimAgenciesEnabled(settingsRaw)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(v.Name, func(t *testing.T) {
			opts := ClaimOptions{Types: v.Options.Types, Diagnostic: v.Options.Diagnostic, DeferredApifyOnly: v.Options.DeferredApifyOnly, AllowApify: v.Options.AllowApify, AllowApifyStarts: v.Options.AllowApifyStarts, JobID: v.Options.JobID}
			if !v.Settings.Enabled && !opts.Diagnostic {
				if v.Selected != "" || v.Error != "" {
					t.Fatal("disabled source claimed")
				}
				return
			}
			types, err := claimTypes(opts.Types)
			if errors.Is(err, ErrJobInput) && v.Error == "input" {
				return
			}
			if err != nil || v.Error != "" {
				t.Fatal("type validation differs", err, v.Error)
			}
			view := claimView{busy: map[int64]bool{}, pages: map[string]string{}, operations: map[string]bool{}}
			for _, j := range jobs {
				if j.Generation == v.Generation && j.Status == "running" && j.CandidateID != nil {
					view.busy[*j.CandidateID] = true
				}
			}
			for id, page := range v.Pages {
				text, present, e := sqliteJSONText(append(append([]byte(`{"page":`), page...), '}'), "page")
				if e != nil {
					t.Fatal(e)
				}
				if !present {
					text = "0"
				}
				view.pages[id] = text
			}
			for _, key := range v.Operations {
				view.operations[key] = true
			}
			var best *Job
			bestPage := false
			for _, j := range jobs {
				if j.Generation != v.Generation {
					continue
				}
				ok, page, e := claimEligible(j, v.Now, v.Settings, opts, types, view)
				if e != nil {
					t.Fatal(e)
				}
				if ok && (best == nil || claimBefore(j, page, *best, bestPage, v.Now)) {
					copy := j
					best = &copy
					bestPage = page
				}
			}
			got := ""
			if best != nil {
				got = best.ID
			}
			if got != v.Selected {
				t.Fatalf("Go selected %q; unchanged source selected %q", got, v.Selected)
			}
		})
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal("scheduler source failed", err)
	}
	if count != 142 {
		t.Fatal("missing source selection vectors", count)
	}
}
