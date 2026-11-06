package parser

import (
	"testing"
	"time"
)

func TestHHDateWindowPreviousFullMoscowDays(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		days int
		from string
	}{{3, "2026-11-01T00:00:00+03:00"}, {7, "2026-10-28T00:00:00+03:00"}, {14, "2026-10-21T00:00:00+03:00"}} {
		from, to := hhDateWindow(now, tc.days)
		if from == nil || to == nil || *from != tc.from || *to != "2026-11-03T23:59:59+03:00" {
			t.Fatalf("days=%d from=%v to=%v", tc.days, from, to)
		}
	}
	from, to := hhDateWindow(now, 0)
	if from != nil || to != nil {
		t.Fatal("all vacancies must not use date bounds")
	}
}

// Проверка диапазона используется только для диагностики Dataset, не для отсева вакансий.
func TestHHDateWindowClassifiesActorRecordsForDiagnostics(t *testing.T) {
	from, to := "2026-11-01T00:00:00+03:00", "2026-11-03T23:59:59+03:00"
	c := hhListingCursor{DateFilterApplied: true, VacancyAgeDays: 3, DateFrom: &from, DateTo: &to}
	for _, v := range []struct {
		raw  string
		want bool
		bad  bool
	}{
		{`{"date_posted":"2026-11-01T00:00:00+03:00"}`, true, false},
		{`{"date_posted":"2026-11-03T23:59:59+03:00"}`, true, false},
		{`{"date_posted":"2026-11-04T00:00:00+03:00"}`, false, false},
		{`{"date_posted":"2026-10-31T23:59:59+03:00"}`, false, false},
		{`{}`, false, true},
	} {
		got, err := c.includesPublication([]byte(v.raw))
		if got != v.want || (err != nil) != v.bad {
			t.Fatalf("%s got=%v err=%v", v.raw, got, err)
		}
	}
	c.VacancyAgeDays = 0
	if ok, err := c.includesPublication([]byte(`{}`)); !ok || err != nil {
		t.Fatal("all dates must accept undated records")
	}
}
