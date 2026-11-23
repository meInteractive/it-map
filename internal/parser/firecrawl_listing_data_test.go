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
