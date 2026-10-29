//go:build legacyinterop

package vacancies

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestVacancyApplicationSummarySource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-vacancy-application-summary.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame struct {
			Name string
			Now  int64
			Apps []struct {
				User, Company string
				Vacancy       *int64
			}
			Vacancies map[string][]Vacancy
			Expected  map[string]map[string]json.RawMessage
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		for _, user := range []string{"Eugene", "Nastya", "Demo"} {
			general := map[string]bool{}
			for _, app := range frame.Apps {
				if app.User == user && app.Vacancy == nil {
					general[app.Company] = true
				}
			}
			before, _ := json.Marshal(frame.Vacancies[user])
			for _, name := range []string{"none", "all", "week", "month", "twoMonths", "archive"} {
				t.Run(frame.Name+"/"+user+"/"+name, func(t *testing.T) {
					var period *Period
					if name != "none" {
						p := Period(name)
						period = &p
					}
					body := BuildList(frame.Vacancies[user], nil, nil, nil, nil, nil, general, period, time.UnixMilli(frame.Now))
					actual, err := json.Marshal(body.ApplicationSummary)
					if err != nil {
						t.Fatal(err)
					}
					var got, want any
					if json.Unmarshal(actual, &got) != nil || json.Unmarshal(frame.Expected[user][name], &want) != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("summary differs: got %s want %s", actual, frame.Expected[user][name])
					}
				})
			}
			after, _ := json.Marshal(frame.Vacancies[user])
			if string(before) != string(after) {
				t.Fatal("summary projection mutated input vacancies")
			}
		}
	}
	if cmd.Wait() != nil || count != 57 {
		t.Fatal("incomplete application summary corpus", count)
	}
}
