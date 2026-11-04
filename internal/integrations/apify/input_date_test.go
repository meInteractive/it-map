package apify

import (
	"net/url"
	"testing"
)

func TestHHInputDateWindowOverridesSourceAndAllRemovesBounds(t *testing.T) {
	from, to := "2026-11-01T00:00:00+03:00", "2026-11-03T23:59:59+03:00"
	for _, tc := range []struct {
		options          HHOptions
		wantFrom, wantTo string
	}{
		{HHOptions{DateFilterApplied: true, DateFrom: &from, DateTo: &to}, from, to},
		{HHOptions{DateFilterApplied: true}, "", ""},
	} {
		input, err := HHInput("https://hh.ru/search/vacancy?employer_id=780654&date_from=old&date_to=old", tc.options)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(input.StartURL)
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Get("date_from") != tc.wantFrom || u.Query().Get("date_to") != tc.wantTo {
			t.Fatalf("unexpected date window: %s", input.StartURL)
		}
	}
	if _, err := HHInput("https://hh.ru/employer/780654", HHOptions{DateFilterApplied: true, DateFrom: &from}); err == nil {
		t.Fatal("half-open date filter accepted")
	}
}
