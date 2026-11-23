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
	got, err := companyListingCards(markdown, "https://careers.company.com/jobs/open?page=2")
	want := []companyListingCard{
		{"Senior Go Engineer", "https://careers.company.com/jobs/123?lang=ru"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCompanyListingRejectsUnverifiedEmpty(t *testing.T) {
	if _, err := companyListingCards("# Careers\n[Home](/)", "https://careers.company.com/jobs"); !errors.Is(err, errFirecrawlListing) {
		t.Fatalf("got %v", err)
	}
}
