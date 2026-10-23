package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"itmap/internal/roles"
)

func int64ptr(n int64) *int64 { return &n }
func strptr(s string) *string { return &s }
func TestVacancyProjectionPreservesLegacyNullAndUserState(t *testing.T) {
	state := VacancyState{Vacancy: Vacancy{ID: 13, CompanyName: "Ромашка", EntityType: "unknown", Title: "Engineering Manager", Role: "Old role", CreatedAt: "2026-10-23 11:00:00", ApplicationStatus: "Откликнулся"}, SalaryFromSQL: sql.NullInt64{Int64: 350000, Valid: true}, RelevanceSQL: sql.NullInt64{Int64: 0, Valid: true}, ApplicationIDSQL: sql.NullInt64{}, FavoriteSQL: 1, HiddenSQL: 0}
	v := state.project()
	if v.SalaryFrom == nil || *v.SalaryFrom != 350000 || v.SalaryTo != nil || v.Relevance == nil || *v.Relevance != 0 || !v.Favorite || v.Hidden || v.EntityType != "company" || v.Roles == nil || v.TelegramSources == nil {
		t.Fatalf("unexpected %+v", v)
	}
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	for _, piece := range []string{`"salaryTo":null`, `"relevance":0`, `"roles":[]`, `"telegramSources":[]`, `"applicationId":null`} {
		if !strings.Contains(string(b), piece) {
			t.Fatalf("missing %s: %s", piece, b)
		}
	}
}
func TestVacancyListPeriodOnlyFiltersVacanciesAndRoles(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	rs := []roles.Role{{ID: 7, Name: "Разработка", Path: "IT → Разработка"}, {ID: 8, Name: "Продукт", Path: "IT → Продукт"}}
	input := []Vacancy{
		{ID: 1, CompanyName: "Ромашка", EntityType: "company", Title: "Old", CreatedAt: "2026-07-21", PublishedAt: strptr("2026-08-01"), Roles: []roles.Role{}, TelegramSources: []TelegramSource{}},
		{ID: 2, CompanyName: "Ромашка", EntityType: "company", Title: "New", CreatedAt: "2026-10-22", PublishedAt: strptr("2026-10-22"), Relevance: int64ptr(0)},
	}
	catalog := []map[string]any{{"name": "Ромашка", "rank": float64(3), "entityType": "company", "middle": []any{100, 200}, "applicationUrl": "https://example.test"}}
	flags := map[string]entityFlags{"Ромашка": {favorite: true, applications: true}}
	selected := map[int64][]roles.Role{2: rs}
	channels := map[int64][]TelegramSource{2: {{ChannelID: 8, ChannelName: "Канал"}}}
	period := Week
	out := BuildList(input, selected, channels, catalog, flags, rs, map[string]bool{"Ромашка": true, "Другие": true}, &period, now)
	if len(out.Vacancies) != 1 || out.Vacancies[0].ID != 2 || out.Vacancies[0].Role != "Продукт, Разработка" || len(out.Vacancies[0].TelegramSources) != 1 {
		t.Fatalf("vacancies %+v", out.Vacancies)
	}
	if len(out.Entities) != 1 || !out.Entities[0].HasVacancies || !out.Entities[0].Favorite || !out.Entities[0].HasApplications {
		t.Fatalf("entities %+v", out.Entities)
	}
	if len(out.RoleTree) != 2 {
		t.Fatalf("roles %+v", out.RoleTree)
	}
	// The response must exclude application summaries for names absent from the vacancy list.
	if len(out.ApplicationSummary) != 1 || out.ApplicationSummary[0].CompanyName != "Ромашка" {
		t.Fatalf("summary %+v", out.ApplicationSummary)
	}
	all := All
	out = BuildList(input, selected, channels, catalog, flags, rs, nil, &all, now)
	if len(out.Vacancies) != 2 {
		t.Fatalf("all must include archive and invalid dates: %+v", out)
	}
}
func TestBlankDatabaseAndUserNeverUsedAsAdmin(t *testing.T) {
	repo := Repository{}
	if _, err := repo.List(context.Background(), "Demo", nil, time.Now()); err == nil {
		t.Fatal("nil db accepted")
	}
}
