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

type vacancyOrderRow struct {
	ID        int64   `json:"id"`
	Company   string  `json:"company"`
	Published *string `json:"published"`
	Loaded    *string `json:"loaded"`
	Created   string  `json:"created"`
}
type vacancyOrderFrame struct {
	Name     string                     `json:"name"`
	Now      int64                      `json:"now"`
	Rows     []vacancyOrderRow          `json:"rows"`
	Expected map[string]json.RawMessage `json:"expected"`
}

func TestMariaDBVacancyRowOrderSourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"company_vacancy_user_state", "company_vacancies", "company_crm", "app_catalog"}
	for _, table := range append(append([]string{}, tables...), "company_applications", "vacancy_roles") {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("row-order fixture requires empty disposable namespace")
		}
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("row-order fixture cleanup failed")
			}
		}
	}
	t.Cleanup(cleanup)
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-row-order.mjs")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Start() != nil {
		t.Fatal("row-order source corpus not started")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	wanted := map[string]bool{"empty": true, "ASCII case": true, "NULL vs empty": true, "Unicode stays case sensitive": true, "embedded NUL": true, "NUL byte lengths": true, "binary published text": true}
	frames := []vacancyOrderFrame{}
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame vacancyOrderFrame
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if wanted[frame.Name] {
			frames = append(frames, frame)
		}
	}
	if cmd.Wait() != nil || count != 105 || len(frames) != 7 {
		t.Fatal("required row-order source cases missing")
	}
	repo := vacancies.Repository{DB: f.reader}
	for _, frame := range frames {
		cleanup()
		if t.Failed() {
			return
		}
		f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[],"agencies":[]}`)
		companies := map[string]bool{}
		states := []map[string]any{}
		for _, r := range frame.Rows {
			if !companies[r.Company] {
				f.exec("INSERT INTO company_crm(company_name) VALUES(?)", r.Company)
				companies[r.Company] = true
			}
			f.exec("INSERT INTO company_vacancies(id,company_name,url,title,published_at,loaded_at,created_at) VALUES(?,?,CONCAT('https://example.invalid/go338/',?), 'CI row',?,?,?)", r.ID, r.Company, r.ID, r.Published, r.Loaded, r.Created)
			f.exec("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES('Demo',?,11,1,0),('Nastya',?,22,0,1)", r.ID, r.ID)
			states = append(states, map[string]any{"user": "Demo", "id": r.ID, "relevance": 11, "favorite": 1, "hidden": 0}, map[string]any{"user": "Nastya", "id": r.ID, "relevance": 22, "favorite": 0, "hidden": 1})
		}
		before := dashboardFingerprint(f)
		for _, user := range []string{"Eugene", "Nastya", "Demo"} {
			raw, _ := json.Marshal(map[string]any{"rows": frame.Rows, "now": frame.Now, "user": user, "states": states})
			reference := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-row-order.mjs", "--reference")
			reference.Stdin = bytes.NewReader(raw)
			output, err := reference.Output()
			if err != nil {
				t.Fatal("row-order source reference failed")
			}
			var expected vacancyOrderFrame
			if json.Unmarshal(output, &expected) != nil || len(expected.Expected) != 6 {
				t.Fatal("invalid row-order source reference")
			}
			for _, name := range []string{"none", "all", "week", "month", "twoMonths", "archive"} {
				t.Run(frame.Name+"/"+user+"/"+name, func(t *testing.T) {
					var period *vacancies.Period
					if name != "none" {
						p := vacancies.Period(name)
						period = &p
					}
					body, err := repo.List(f.ctx, user, period, time.UnixMilli(frame.Now))
					if err != nil {
						t.Fatal("readonly vacancy rows failed", err)
					}
					encoded, err := json.Marshal(body.Vacancies)
					if err != nil {
						t.Fatal(err)
					}
					var actual, want any
					if json.Unmarshal(encoded, &actual) != nil || json.Unmarshal(expected.Expected[name], &want) != nil || !reflect.DeepEqual(actual, want) {
						t.Fatal("native order/raw date projection/period/user state differs from source SQLite")
					}
					if before != dashboardFingerprint(f) {
						t.Fatal("row-order read changed one of 72 tables")
					}
				})
			}
		}
	}
	t.Log("PASS: exact source SQLite vacancy ordering vs readonly MariaDB, 126 user/period/NUL/raw-date cases, 72 tables unchanged")
}
