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

type vacancySourceOrderFrame struct {
	Name     string `json:"name"`
	Channels []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
	} `json:"channels"`
	Sources []struct {
		ID      int64  `json:"id"`
		Channel int64  `json:"channel"`
		Message string `json:"message"`
		Contact string `json:"contact"`
	} `json:"sources"`
	Expected map[string]json.RawMessage `json:"expected"`
}

func TestMariaDBVacancyTelegramSourceOrderDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"vacancy_telegram_sources", "telegram_channels", "company_vacancies", "company_crm", "app_catalog"}
	for _, table := range append(append([]string{}, tables...), "company_applications", "company_vacancy_user_state", "vacancy_roles") {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("Telegram order fixture requires empty disposable namespace")
		}
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("Telegram order cleanup failed")
			}
		}
	}
	t.Cleanup(cleanup)
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-source-order.mjs")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	frames := []vacancySourceOrderFrame{}
	for {
		var frame vacancySourceOrderFrame
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
		t.Fatal("required Telegram source cases missing")
	}
	repo := vacancies.Repository{DB: f.reader}
	for _, frame := range frames {
		cleanup()
		if t.Failed() {
			return
		}
		f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[],"agencies":[]}`)
		f.exec("INSERT INTO company_crm(company_name) VALUES('CI GO340 employer')")
		f.exec("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'CI GO340 employer','https://example.invalid/go340','CI source order')")
		for _, c := range frame.Channels {
			f.exec("INSERT INTO telegram_channels(id,name,url,username,normalized_username) VALUES(?,?,?,?,CONCAT('ci_go340_',?))", c.ID, c.Name, c.URL, c.Username, c.ID)
		}
		for _, s := range frame.Sources {
			f.exec("INSERT INTO vacancy_telegram_sources(id,vacancy_id,channel_id,message_url,telegram_contact) VALUES(?,1,?,?,?)", s.ID, s.Channel, s.Message, s.Contact)
		}
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		source := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-source-order.mjs", "--reference")
		source.Stdin = bytes.NewReader(raw)
		output, err := source.Output()
		if err != nil {
			t.Fatal("Telegram source reference failed", err)
		}
		var expected vacancySourceOrderFrame
		if json.Unmarshal(output, &expected) != nil || len(expected.Expected) != 3 {
			t.Fatal("invalid Telegram source reference")
		}
		before := dashboardFingerprint(f)
		for _, user := range []string{"Eugene", "Nastya", "Demo"} {
			for _, view := range []string{"list", "detail", "company"} {
				t.Run(frame.Name+"/"+user+"/"+view, func(t *testing.T) {
					var actual any
					switch view {
					case "list":
						body, err := repo.List(f.ctx, user, nil, time.Now())
						if err != nil || len(body.Vacancies) != 1 {
							t.Fatal("readonly Telegram list failed", err)
						}
						actual = body.Vacancies[0].TelegramSources
					case "detail":
						body, err := repo.DetailByID(f.ctx, user, 1)
						if err != nil {
							t.Fatal("readonly Telegram detail failed", err)
						}
						actual = body.Vacancy.TelegramSources
					case "company":
						body, err := repo.CompanyVacancies(f.ctx, user, "CI GO340 employer")
						if err != nil || len(body) != 1 {
							t.Fatal("readonly Telegram company failed", err)
						}
						actual = body[0].TelegramSources
					}
					encoded, err := json.Marshal(actual)
					if err != nil {
						t.Fatal(err)
					}
					var got, want any
					if json.Unmarshal(encoded, &got) != nil || json.Unmarshal(expected.Expected[view], &want) != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("native source order/fields differ: got %s want %s", encoded, expected.Expected[view])
					}
					if before != dashboardFingerprint(f) {
						t.Fatal("Telegram read changed one of 72 tables")
					}
				})
			}
		}
	}
	t.Log("PASS: source Telegram display order, 81 read-only view/user cases, 72 tables unchanged")
}
