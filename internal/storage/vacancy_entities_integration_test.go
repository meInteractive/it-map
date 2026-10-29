//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

func TestMariaDBVacancyEntitySourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"company_application_status_history", "company_applications", "company_user_state", "company_vacancy_user_state", "company_vacancies", "company_crm", "app_catalog"}
	for _, table := range tables {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("entity fixture requires empty disposable namespace")
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("entity fixture cleanup failed")
			}
		}
	})
	const agency = "CI337 agency"
	const company = "CI337 A"
	f.exec("INSERT INTO company_crm(company_name) VALUES(?),(?)", company, agency)
	f.exec("INSERT INTO company_vacancies(id,company_name,entity_type,real_company_name,url,title) VALUES(33701,?,'agency',?,'https://example.invalid/go337','CI vacancy')", agency, company)
	f.exec("INSERT INTO company_user_state(user_name,company_name,favorite,hidden) VALUES('Demo',?,1,0),('Nastya',?,0,1)", company, agency)
	f.exec("INSERT INTO company_applications(user_name,company_name,status) VALUES('Demo',?,'CI synthetic status')", company)
	defaults := `{"companies":[{"name":"CI337 B","rank":90},{"name":"CI337 A","rank":80}],"agencies":[{"name":"CI337 agency","rank":"9"}]}`
	fields := `{"companies":[{"name":"CI337 B","ratingInputs":{"roleFit":10}},{"name":"CI337 A","middle":["100","200"],"senior":[300,100],"lead":null,"head":[null,"400.5"],"applicationUrl":"  https://example.invalid/apply  ","resume":"CI_PRIVATE_ENTITY_MUST_NOT_APPEAR","ratingInputs":{"roleFit":0}}],"agencies":[{"name":"CI337 agency","rank":"5","middle":["12","30"],"applicationUrl":" \t https://example.invalid/agency \t"}]}`
	malformed := "{"
	withoutAgencies := `{"companies":[{"name":"CI337 A"}]}`
	variants := []struct {
		Name string
		Raw  *string
	}{{"defaults", &defaults}, {"salary and ranks", &fields}, {"missing", nil}, {"malformed", &malformed}, {"missing agencies", &withoutAgencies}}
	repo := vacancies.Repository{DB: f.reader}
	for _, variant := range variants {
		f.exec("DELETE FROM app_catalog")
		if variant.Raw != nil {
			f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", *variant.Raw)
		}
		raw, _ := json.Marshal(map[string]any{"raw": variant.Raw, "names": []string{agency, company}})
		cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-vacancy-entities.mjs", "--reference")
		cmd.Stdin = bytes.NewReader(raw)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal("entity SQLite reference failed")
		}
		var reference struct {
			Expected json.RawMessage
			Error    bool
		}
		if json.Unmarshal(out, &reference) != nil {
			t.Fatal("invalid source entity reference")
		}
		var expected any
		if !reference.Error && json.Unmarshal(reference.Expected, &expected) != nil {
			t.Fatal("invalid source entity directory")
		}
		before := dashboardFingerprint(f)
		for _, user := range []string{"Eugene", "Nastya", "Demo"} {
			for _, view := range []string{"list", "detail"} {
				t.Run(variant.Name+"/"+user+"/"+view, func(t *testing.T) {
					var entities any
					var failure error
					if view == "list" {
						period := vacancies.All
						body, err := repo.List(f.ctx, user, &period, time.Now())
						entities, failure = body.Entities, err
					} else {
						body, err := repo.DetailByID(f.ctx, user, 33701)
						entities, failure = body.Entities, err
					}
					if (failure != nil) != reference.Error {
						t.Fatal("native entity error differs from source")
					}
					if failure == nil {
						encoded, err := json.Marshal(entities)
						if err != nil {
							t.Fatal(err)
						}
						if strings.Contains(string(encoded), "CI_PRIVATE_ENTITY_MUST_NOT_APPEAR") {
							t.Fatal("entity directory leaked unrelated catalog fields")
						}
						var actual []map[string]any
						if json.Unmarshal(encoded, &actual) != nil {
							t.Fatal("invalid native entity JSON")
						}
						if view == "list" {
							for _, e := range actual {
								if e["favorite"] != (user == "Demo" && e["name"] == company) || e["hidden"] != (user == "Nastya" && e["name"] == agency) || e["hasVacancies"] != true || e["hasApplications"] != (user == "Demo" && e["name"] == company) {
									t.Fatal("normalized entities lost user flags")
								}
								for _, key := range []string{"favorite", "hidden", "hasVacancies", "hasApplications"} {
									delete(e, key)
								}
							}
						}
						// Декодируем повторно, чтобы сравнение не зависело от Go-типа среза.
						normalized, _ := json.Marshal(actual)
						var body any
						if json.Unmarshal(normalized, &body) != nil || !reflect.DeepEqual(body, expected) {
							t.Fatal("native entity fields/order/shape differ from unchanged source")
						}
					}
					if before != dashboardFingerprint(f) {
						t.Fatal("entity read changed one of 72 tables")
					}
				})
			}
		}
	}
	t.Log("PASS: source normalized entities vs readonly MariaDB list/detail, 30 catalog/user cases, 72 tables unchanged")
}
