package vacancies

import (
	"errors"
	"strings"
	"testing"
)

func baseInput() Input {
	return Input{CompanyName: "Example", URL: "https://example.com", Title: "Руководитель разработки", Level: "senior", DescriptionMD: "Полный текст вакансии", PublishedAt: "2026-10-23", RoleIDs: []int64{3, 3, 7}}
}
func TestNormalizeInputCanonicalFields(t *testing.T) {
	in, err := NormalizeInput(baseInput(), false)
	if err != nil || in.URL != "https://example.com/" || in.Level != "Senior" || len(in.RoleIDs) != 2 {
		t.Fatal(in, err)
	}
	in.EntityType = "channel"
	in.CompanyName = ""
	id := int64(10)
	in.TelegramChannelID = &id
	in.TelegramMessageURL = "https://t.me/demo/10"
	normalized, err := NormalizeInput(in, false)
	if err != nil || normalized.CompanyName != "__telegram_channel__:10" || normalized.EntityType != "channel" {
		t.Fatal(normalized, err)
	}
	in = baseInput()
	in.EntityType = "agency"
	in.RealCompanyName = "Actual"
	in.Level = "HEAD"
	normalized, err = NormalizeInput(in, false)
	if err != nil || normalized.RealCompanyName != "Actual" || normalized.Level != "Head" {
		t.Fatal(normalized, err)
	}
}
func TestNormalizeInputRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Input)
	}{
		{"url scheme", func(in *Input) { in.URL = "file:///etc/passwd" }},
		{"missing host", func(in *Input) { in.URL = "https:///" }},
		{"userinfo", func(in *Input) { in.URL = "https://user:pass@example.com" }},
		{"salary", func(in *Input) { a, b := int64(200), int64(100); in.SalaryFrom = &a; in.SalaryTo = &b }},
		{"invalid day", func(in *Input) { in.PublishedAt = "2026-02-30" }},
		{"missing title", func(in *Input) { in.Title = "  " }},
		{"wrong level", func(in *Input) { in.Level = "Intern" }},
		{"invalid relevance", func(in *Input) { n := int64(101); in.Relevance = &n }},
		{"duplicate long title", func(in *Input) { in.Title = strings.Repeat("и", 301) }},
		{"utf16 limit", func(in *Input) { in.CoverLetterSubject = strings.Repeat("😀", 257) }},
		{"role ID", func(in *Input) { in.RoleIDs = []int64{-1} }},
		{"channel missing id", func(in *Input) { in.EntityType = "channel" }},
		{"telegram no message URL", func(in *Input) { id := int64(5); in.TelegramChannelID = &id }},
		{"empty markdown", func(in *Input) { in.DescriptionMD = " \t" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			tc.change(&in)
			if _, err := NormalizeInput(in, false); !errors.Is(err, ErrInvalidVacancyInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}
func TestNormalizeInputAllowsEmptyLegacyFieldsOnlyWhenRequested(t *testing.T) {
	in := baseInput()
	in.Title = ""
	in.Level = ""
	in.DescriptionMD = ""
	if _, err := NormalizeInput(in, false); err == nil {
		t.Fatal("unexpected accepted empty fields")
	}
	if _, err := NormalizeInput(in, true); err != nil {
		t.Fatal(err)
	}
}
