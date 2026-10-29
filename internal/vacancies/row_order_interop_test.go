//go:build legacyinterop

package vacancies

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestVacancyRowOrderSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-vacancy-row-order.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Start() != nil {
		t.Fatal("vacancy row source not started")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame struct {
			Name string
			Now  int64
			Rows []struct {
				ID                int64
				Company           string
				Published, Loaded *string
				Created           string
			}
			Expected map[string]json.RawMessage
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(frame.Name, func(t *testing.T) {
			states := []VacancyState{}
			for _, r := range frame.Rows {
				s := VacancyState{Vacancy: Vacancy{ID: r.ID, CompanyName: r.Company, EntityType: "company", Title: "CI row", CreatedAt: r.Created}}
				if r.Published != nil {
					s.PublishedSQL = sql.NullString{String: *r.Published, Valid: true}
				}
				if r.Loaded != nil {
					s.LoadedSQL = sql.NullString{String: *r.Loaded, Valid: true}
				}
				states = append(states, s)
			}
			before, _ := json.Marshal(states)
			ordered := orderedVacancyRows(states)
			after, _ := json.Marshal(states)
			if string(before) != string(after) {
				t.Fatal("row ordering mutated source values")
			}

			for _, name := range []string{"none", "all", "week", "month", "twoMonths", "archive"} {
				var period *Period
				if name != "none" {
					p := Period(name)
					period = &p
				}
				body := BuildList(ordered, nil, nil, nil, nil, nil, nil, period, time.UnixMilli(frame.Now))
				raw, err := json.Marshal(body.Vacancies)
				if err != nil {
					t.Fatal(err)
				}
				var actual, want any
				if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(frame.Expected[name], &want) != nil || !reflect.DeepEqual(actual, want) {
					t.Fatalf("source row order/raw date projection differs for period %s", name)
				}
			}
		})
	}
	if cmd.Wait() != nil || count != 105 {
		t.Fatal("incomplete vacancy row source corpus", count)
	}
}
