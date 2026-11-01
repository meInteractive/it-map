package parser

import (
	"testing"
)

func TestImportExtractionMapping(t *testing.T) {
	data := map[string]any{"title": " Engineer ", "description_md": "Description", "salary_from": float64(100), "salary_to": float64(200), "salary_currency": "RUB", "salary_period": "month", "level": "senior", "published_at": "2026-10-01"}
	in, err := mapImportExtraction("CI_COMPANY", "https://hh.ru/vacancy/123", "2026-10-02T00:00:00Z", []int64{2}, data)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Engineer" || in.Level != "Senior" || in.SalaryFrom == nil || *in.SalaryFrom != 100 || in.SalaryTo == nil || *in.SalaryTo != 200 || in.LoadedAt != "2026-10-02" {
		t.Fatalf("unexpected input: %+v", in)
	}
	data["salary_currency"] = "USD"
	in, err = mapImportExtraction("CI_COMPANY", "https://hh.ru/vacancy/123", "2026-10-02T00:00:00Z", []int64{2}, data)
	if err != nil || in.SalaryFrom != nil || in.SalaryTo != nil {
		t.Fatalf("foreign currency mapped: %+v %v", in, err)
	}
	data["level"] = "unknown"
	in, err = mapImportExtraction("CI_COMPANY", "https://hh.ru/vacancy/123", "2026-10-02T00:00:00Z", []int64{2}, data)
	if err != nil || in.Level != "" {
		t.Fatalf("unmapped level: %+v %v", in, err)
	}
	data["salary_currency"] = "RUB"
	data["salary_from"] = 100.5
	if _, err = mapImportExtraction("CI_COMPANY", "https://hh.ru/vacancy/123", "2026-10-02T00:00:00Z", []int64{2}, data); err == nil {
		t.Fatal("fractional salary accepted")
	}
}
