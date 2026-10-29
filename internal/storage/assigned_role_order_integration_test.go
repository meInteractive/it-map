//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"itmap/internal/roles"
	"itmap/internal/vacancies"
)

func TestMariaDBAssignedRoleOrderDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	tables := []string{"vacancy_role_assignments", "company_vacancy_user_state", "company_vacancies", "vacancy_roles", "company_crm", "app_catalog"}
	for _, table := range tables {
		var count int
		if f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
			t.Fatal("role-order fixture requires empty disposable namespace")
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range tables {
			query := "DELETE FROM " + table
			if table == "vacancy_roles" {
				query += " ORDER BY id DESC"
			}
			if _, err := f.db.ExecContext(ctx, query); err != nil {
				t.Error("role-order fixture cleanup failed")
			}
		}
	})
	const company = "CI GO336 synthetic employer"
	f.exec("INSERT INTO company_crm(company_name) VALUES(?)", company)
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(33600,?)", company)
	catalog, _ := json.Marshal(map[string]any{"companies": []map[string]any{{"name": company, "jobs": []any{}, "offices": []any{}}}, "agencies": []any{}})
	f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", string(catalog))
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name) VALUES(33600,'CI_go336_root','CI корень')")
	for i, name := range []string{"Z", "Я", "A", "Ёлка", "Елка", "Å", "A\u030a", "😀"} {
		f.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id) VALUES(?,?,?,33600)", 33601+i, "CI_go336_"+name, name)
	}
	type assignment struct {
		VacancyID int64 `json:"vacancyId"`
		RoleID    int64 `json:"roleId"`
	}
	assignments := []assignment{}
	input := []map[string]any{}
	for i := int64(1); i <= 3; i++ {
		id := 33600 + i
		f.exec("INSERT INTO company_vacancies(id,company_name,url,title,role) VALUES(?,?,?,?,?)", id, company, "https://example.invalid/go336/"+string(rune('0'+i)), "CI vacancy", "CI прежняя роль")
		input = append(input, map[string]any{"id": id, "role": "CI прежняя роль"})
		limit := 8
		if i == 2 {
			limit = 4
		}
		if i == 3 {
			limit = 0
		}
		for r := 0; r < limit; r++ {
			roleID := int64(33601 + r)
			f.exec("INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)", id, roleID)
			assignments = append(assignments, assignment{id, roleID})
		}
		f.exec("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,favorite,hidden) VALUES('Demo',?,1,0),('Nastya',?,0,1)", id, id)
	}
	directory, err := (roles.Repository{DB: f.reader}).Read(f.ctx)
	if err != nil {
		t.Fatal("cannot read role fixture catalog")
	}
	raw, _ := json.Marshal(map[string]any{"roles": directory.Roles, "assignments": assignments, "vacancies": input})
	cmd := exec.CommandContext(f.ctx, "node", "../../scripts/check-assigned-role-order.mjs", "--reference")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal("assigned role SQLite reference failed")
	}
	var oracle struct {
		Expected []struct {
			ID    int64
			Role  string
			Roles []roles.Role
		}
	}
	if json.Unmarshal(out, &oracle) != nil || len(oracle.Expected) != 3 {
		t.Fatal("invalid assigned role SQLite reference")
	}
	before := dashboardFingerprint(f)
	repo := vacancies.Repository{DB: f.reader}
	for _, user := range []string{"Eugene", "Nastya", "Demo"} {
		for _, view := range []string{"list", "detail", "company"} {
			t.Run(user+"/"+view, func(t *testing.T) {
				actual := []vacancies.Vacancy{}
				switch view {
				case "list":
					period := vacancies.All
					body, e := repo.List(f.ctx, user, &period, time.Now())
					if e != nil {
						t.Fatal("readonly vacancy list failed", e)
					}
					actual = body.Vacancies
				case "company":
					body, e := repo.CompanyVacancies(f.ctx, user, company)
					if e != nil {
						t.Fatal("readonly company vacancies failed", e)
					}
					for _, v := range body {
						actual = append(actual, v.Vacancy)
					}
				case "detail":
					for _, expected := range oracle.Expected {
						body, e := repo.DetailByID(f.ctx, user, expected.ID)
						if e != nil {
							t.Fatal("readonly vacancy detail failed", e)
						}
						actual = append(actual, body.Vacancy.Vacancy)
					}
				}
				if len(actual) != 3 {
					t.Fatal("role fixture vacancy count differs")
				}
				for _, expected := range oracle.Expected {
					var got *vacancies.Vacancy
					for i := range actual {
						if actual[i].ID == expected.ID {
							got = &actual[i]
							break
						}
					}
					if got == nil || got.Role != expected.Role || !reflect.DeepEqual(got.Roles, expected.Roles) {
						t.Fatal("native assigned roles differ from unchanged source")
					}
					if got.Favorite != (user == "Demo") || got.Hidden != (user == "Nastya") {
						t.Fatal("role projection lost user isolation")
					}
				}
				if before != dashboardFingerprint(f) {
					t.Fatal("readonly roles changed one of 72 tables")
				}
			})
		}
	}
	t.Log("PASS: source SQLite assigned roles vs readonly MariaDB list/detail/company, 9 role/user cases, 72 tables unchanged")
}
