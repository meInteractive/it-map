package vacancies

import (
	"encoding/json"
	"testing"

	"itmap/internal/roles"
)

func TestVacancyExcelInput(t *testing.T) {
	owners := vacancyImportOwners{
		companies:   map[int64]string{12: "CI Company"},
		agencies:    map[int64]string{13: "CI Agency"},
		agencyNames: map[string]bool{"CI Agency": true},
	}
	valid := `{"companyId":12,"entityType":"Компания","url":"https://EXAMPLE.invalid:443","title":"CI title","salaryFrom":100,"salaryTo":200,"publishedAt":"2026-11-27","relevance":"0","coverLetterMd":""}`
	row, err := owners.input(json.RawMessage(valid), roles.Catalog{})
	if err != nil || row.URL != "https://example.invalid/" || row.CompanyName != "CI Company" ||
		row.Relevance == nil || *row.Relevance != 0 || !row.HasLetter || row.HasSubject {
		t.Fatalf("некорректная строка: %+v, %v", row, err)
	}
	agency := `{"agencyId":13,"realCompanyId":12,"entityType":"Агентство","url":"https://example.invalid/vacancy","relevance":""}`
	row, err = owners.input(json.RawMessage(agency), roles.Catalog{})
	if err != nil || row.EntityType != "agency" || row.CompanyName != "CI Agency" || row.RealCompanyName != "CI Company" || row.Relevance != nil {
		t.Fatalf("некорректная связь агентства и работодателя: %+v, %v", row, err)
	}
	for _, payload := range []string{
		`{"companyId":12,"url":"https://example.invalid","publishedAt":"2026-02-30"}`,
		`{"companyId":12,"url":"https://example.invalid","coverLetterScope":"personal"}`,
		`{"companyId":12,"url":"https://example.invalid","relevance":101}`,
		`{"companyId":12,"url":"https://example.invalid","salaryFrom":200,"salaryTo":100}`,
		`{"companyId":12,"url":"https://example.invalid","role":"CI unknown"}`,
		`{"companyId":99,"url":"https://example.invalid"}`,
	} {
		if _, err := owners.input(json.RawMessage(payload), roles.Catalog{}); err == nil {
			t.Fatal("некорректная строка принята")
		}
	}
}
