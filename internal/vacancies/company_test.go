package vacancies

import (
	"encoding/json"
	"strings"
	"testing"

	"itmap/internal/roles"
)

func TestCompanyVacancyJSONShape(t *testing.T) {
	got := CompanyVacancy{
		Vacancy: Vacancy{ID: 3, CompanyName: "Example", Title: "Role", Roles: []roles.Role{}, TelegramSources: []TelegramSource{}},
		URL:     "https://example.org/job", DescriptionMD: "**description**", CoverLetterScope: "shared", TelegramSources: []DetailTelegramSource{},
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err = json.Unmarshal(body, &row); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"id", "companyName", "url", "descriptionMd", "coverLetterSubject", "coverLetterMd", "coverLetterScope", "lastCheckedAt", "applicationId", "applicationStatus", "roles", "telegramSources", "updatedAt"} {
		if _, ok := row[name]; !ok {
			t.Errorf("missing legacy response field %q", name)
		}
	}
	if strings.Count(string(body), `"telegramSources"`) != 1 {
		t.Fatalf("duplicate telegramSources: %s", body)
	}
	if _, ok := row["revision"]; ok {
		t.Fatal("legacy company list must not introduce detail revision")
	}
}
