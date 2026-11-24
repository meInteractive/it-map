package parser

import (
	"errors"
	"reflect"
	"testing"
)

func TestCompanyListingCards(t *testing.T) {
	const markdown = `
[Senior Go Engineer](/jobs/123?utm_source=site&lang=ru#apply)
[Senior Go Engineer](/jobs/123?lang=ru)
[Team Lead](/vacancies/ops)
[Other title](/vacancies/ops)
![Illustration](/jobs/image)
[Other company](https://other.company.com/jobs/123)
[All openings](/jobs/all)
[Careers](/jobs/start)
[Current page](/jobs/open)
[More](/jobs/open?page=2)
`
	got, err := firecrawlListingCards("companySite", markdown, "https://careers.company.com/jobs/open?page=2")
	want := []companyListingCard{
		{"Senior Go Engineer", "https://careers.company.com/jobs/123?lang=ru"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCompanyListingRejectsUnverifiedEmpty(t *testing.T) {
	if _, err := firecrawlListingCards("companySite", "# Careers\n[Home](/)", "https://careers.company.com/jobs"); !errors.Is(err, errFirecrawlListing) {
		t.Fatalf("got %v", err)
	}
}

func TestCompanyListingAcceptsSavedGenericDefaults(t *testing.T) {
	const address = "https://careers.company.com/jobs"
	for _, tc := range []struct {
		config string
		want   bool
	}{
		{`{}`, true},
		{`{"adapter":"generic","html":false,"titleClass":"","detailPathPrefix":"","allowedHosts":[ ],"pagination":null,"useEnhancedProxy":false}`, true},
		{`{"adapter":"selectel"}`, false},
		{`{"adapter":"yoomoney","html":true}`, false},
		{`{"allowedHosts":["example.com"]}`, false},
		{`{"pagination":{"queryParam":"page"}}`, false},
		{`{"detailPathPrefix":"/careers/"}`, false},
		{`{"unknown":null}`, false},
		{`null`, false},
	} {
		if _, ok := genericCompanyListingHost(address, tc.config); ok != tc.want {
			t.Errorf("config %s admitted=%t, want %t", tc.config, ok, tc.want)
		}
	}
}

func TestGetmatchListingUsesGetmatchVacancyPaths(t *testing.T) {
	const listing = "https://getmatch.ru/companies/ci"
	if _, ok := firecrawlListingHost("getmatch", listing, `{"adapter":"getmatch"}`); !ok {
		t.Fatal("Getmatch source rejected")
	}
	if _, ok := firecrawlListingHost("getmatch", "https://getmatch.ru.company.com/companies/ci", `{"adapter":"getmatch"}`); ok {
		t.Fatal("unrelated host admitted")
	}
	cards, err := firecrawlListingCards("getmatch", "[Engineer](/vacancies/ci-42)\n[Other](/jobs/123)\n[External](https://other.company.com/vacancies/42)", listing)
	if err != nil || len(cards) != 1 || cards[0].URL != "https://getmatch.ru/vacancies/ci-42" {
		t.Fatalf("cards=%+v err=%v", cards, err)
	}
}

func TestGetmatchExplicitEmptyAndUnavailable(t *testing.T) {
	const page = "https://getmatch.ru/companies/ci"
	for _, tc := range []struct {
		markdown string
		want     int
		err      error
	}{
		{"# Вакансии компании CI\n\n**Вакансий пока нет.**", 0, nil},
		{"# Вакансии компании CI\n\n[Вакансии](/vacancies?s=menu)", 0, errFirecrawlListing},
		{"# Вакансии компании CI\n\nВ Талантиксе нет открытых вакансий.", 0, errFirecrawlListing},
		{"# Не смогли загрузить данные\n\nВакансий пока нет.", 0, errGetmatchListingUnavailable},
		{"[Go Engineer](</vacancies/42> \"Job title\")", 1, nil},
	} {
		cards, err := firecrawlListingCards("getmatch", tc.markdown, page)
		if !errors.Is(err, tc.err) || len(cards) != tc.want {
			t.Errorf("listing %q: cards=%v err=%v", tc.markdown, cards, err)
		}
	}
}
