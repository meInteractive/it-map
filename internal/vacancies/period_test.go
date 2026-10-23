package vacancies

import (
	"testing"
	"time"
)

func TestPublicationPeriodMatchesLegacy(t *testing.T) {
	// 2026-10-23 in Moscow while still Oct 22 in UTC.
	now := time.Date(2026, 10, 22, 22, 15, 0, 0, time.UTC)
	cases := []struct {
		p     Period
		dates Dates
		want  bool
	}{
		{Week, Dates{PublishedAt: "2026-10-17"}, true},
		{Week, Dates{PublishedAt: "2026-10-16"}, false},
		{Week, Dates{PublishedAt: "2026-10-24"}, false},
		{Month, Dates{PublishedAt: "2026-09-23"}, true},
		{Month, Dates{PublishedAt: "2026-09-22"}, false},
		{TwoMonths, Dates{PublishedAt: "2026-08-23"}, true},
		{Archive, Dates{PublishedAt: "2026-08-22"}, true},
		{Archive, Dates{PublishedAt: "2026-08-23"}, false},
		{Week, Dates{PublishedAt: "bad", LoadedAt: "2026-10-21"}, true},
		{Week, Dates{CreatedAt: "2026-10-23 09:20:00"}, true},
		{Week, Dates{}, false},
		{All, Dates{}, true},
		{All, Dates{PublishedAt: "2040-01-01"}, true},
	}
	for _, tc := range cases {
		if got := MatchesPublication(tc.p, tc.dates, now); got != tc.want {
			t.Errorf("%s %v: got %t want %t", tc.p, tc.dates, got, tc.want)
		}
	}
}
func TestMonthEndClampingLeapYear(t *testing.T) {
	now := time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)
	if !MatchesPublication(Month, Dates{PublishedAt: "2024-02-29"}, now) {
		t.Fatal("February 29 should be included")
	}
	if MatchesPublication(Month, Dates{PublishedAt: "2024-02-28"}, now) {
		t.Fatal("February 28 should be excluded")
	}
	if _, ok := PublicationDate("2024-02-30"); ok {
		t.Fatal("invalid calendar date")
	}
}
func TestParsePeriod(t *testing.T) {
	for _, good := range []string{"", "week", "month", "twoMonths", "archive", "all"} {
		if _, err := ParsePeriod(good); err != nil {
			t.Fatal(good, err)
		}
	}
	for _, bad := range []string{"ALL", "yesterday", "archived", " Week "} {
		if _, err := ParsePeriod(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
