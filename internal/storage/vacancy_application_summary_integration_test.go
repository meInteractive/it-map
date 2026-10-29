//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

type vacancySummaryFrame struct {
	Name string `json:"name"`
	Now  int64  `json:"now"`
	Rows []struct {
		ID        int64   `json:"id"`
		Company   string  `json:"company"`
		Real      string  `json:"real"`
		Published *string `json:"published"`
		Created   string  `json:"created"`
	} `json:"rows"`
	Apps []struct {
		User    string `json:"user"`
		Company string `json:"company"`
		Vacancy *int64 `json:"vacancy"`
	} `json:"apps"`
	Expected map[string]map[string]json.RawMessage `json:"expected"`
}

func TestMariaDBVacancyApplicationSummarySourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"company_applications", "company_vacancies", "company_crm", "app_catalog"}
	for _, table := range append(append([]string{}, tables...), "company_vacancy_user_state", "vacancy_roles") {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("summary fixture requires empty disposable namespace")
		}
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("summary fixture cleanup failed")
			}
		}
	}
	t.Cleanup(cleanup)
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-application-summary.mjs")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	frames := []vacancySummaryFrame{}
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame vacancySummaryFrame
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if count <= 9 {
			frames = append(frames, frame)
		}
	}
	if cmd.Wait() != nil || count != 57 || len(frames) != 9 {
		t.Fatal("required summary source corpus missing")
	}
	repo := vacancies.Repository{DB: f.reader}
	for _, frame := range frames {
		cleanup()
		if t.Failed() {
			return
		}
		f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[],"agencies":[]}`)
		names := map[string]bool{}
		for _, row := range frame.Rows {
			names[row.Company] = true
			names[row.Real] = true
		}
		for _, app := range frame.Apps {
			names[app.Company] = true
		}
		for name := range names {
			f.exec("INSERT INTO company_crm(company_name) VALUES(?)", name)
		}
		for _, row := range frame.Rows {
			f.exec("INSERT INTO company_vacancies(id,company_name,real_company_name,url,title,published_at,created_at) VALUES(?,?,?,CONCAT('https://example.invalid/go339/',?),'CI summary',?,?)", row.ID, row.Company, row.Real, row.ID, row.Published, row.Created)
		}
		for _, app := range frame.Apps {
			f.exec("INSERT INTO company_applications(user_name,company_name,vacancy_id) VALUES(?,?,?)", app.User, app.Company, app.Vacancy)
		}
		// Обе системы получают те же синтетические строки; источник заново вычисляет DTO.
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		source := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-application-summary.mjs", "--reference")
		source.Stdin = bytes.NewReader(raw)
		output, err := source.Output()
		if err != nil {
			t.Fatal("summary source reference failed", err)
		}
		var expected vacancySummaryFrame
		if json.Unmarshal(output, &expected) != nil || len(expected.Expected) != 3 {
			t.Fatal("invalid summary source reference")
		}
		before := dashboardFingerprint(f)
		for _, user := range []string{"Eugene", "Nastya", "Demo"} {
			for _, name := range []string{"none", "all", "week", "month", "twoMonths", "archive"} {
				t.Run(frame.Name+"/"+user+"/"+name, func(t *testing.T) {
					var period *vacancies.Period
					if name != "none" {
						p := vacancies.Period(name)
						period = &p
					}
					body, err := repo.List(f.ctx, user, period, time.UnixMilli(frame.Now))
					if err != nil {
						t.Fatal("readonly summary failed", err)
					}
					encoded, err := json.Marshal(body.ApplicationSummary)
					if err != nil {
						t.Fatal(err)
					}
					var got, want any
					if json.Unmarshal(encoded, &got) != nil || json.Unmarshal(expected.Expected[user][name], &want) != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("native summary order/user/period differs: got %s want %s", encoded, expected.Expected[user][name])
					}
					if before != dashboardFingerprint(f) {
						t.Fatal("summary read changed one of 72 tables")
					}
				})
			}
		}
	}
	t.Log("PASS: source applicationSummary order, 162 user/period cases, SELECT-only and 72 tables unchanged")
}
