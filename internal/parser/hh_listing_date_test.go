package parser

import (
	"strconv"
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

// GO-293: окно Actor охватывает ПРЕДЫДУЩИЕ полные московские сутки.
// Это проверка потенциальных календарных пропусков, а не доказательство
// отсутствующих записей HH или разрешение на платную догрузку.
func TestGO293SevenDayScheduleAndThreeDayWindowGaps(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	windowDays := func(t *testing.T, at time.Time) map[string]bool {
		t.Helper()
		from, to := hhDateWindow(at.UTC(), 3)
		if from == nil || to == nil {
			t.Fatal("three-day Actor must have bounded dates")
		}
		start, err := time.Parse(time.RFC3339, *from)
		if err != nil {
			t.Fatal(err)
		}
		end, err := time.Parse(time.RFC3339, *to)
		if err != nil {
			t.Fatal(err)
		}
		dates := map[string]bool{}
		for day := start.In(moscow); !day.After(end.In(moscow)); day = day.AddDate(0, 0, 1) {
			dates[day.Format("2006-01-02")] = true
		}
		if len(dates) != 3 {
			t.Fatalf("expected three distinct full calendar days, got %v", dates)
		}
		return dates
	}
	for _, start := range []time.Time{
		time.Date(2026, 11, 4, 15, 0, 0, 0, moscow),
		time.Date(2026, 12, 30, 15, 0, 0, 0, moscow),
	} {
		for _, tc := range []struct {
			interval int
			wantGaps int
		}{
			{1, 0}, {3, 0}, {7, 4},
		} {
			t.Run(start.Format("2006-01-02")+"/interval_"+strconv.Itoa(tc.interval), func(t *testing.T) {
				next := start.AddDate(0, 0, tc.interval)
				first, second := windowDays(t, start), windowDays(t, next)
				gaps := []string{}
				for day := start; day.Before(next); day = day.AddDate(0, 0, 1) {
					d := day.Format("2006-01-02")
					if !first[d] && !second[d] {
						gaps = append(gaps, d)
					}
				}
				if len(gaps) != tc.wantGaps {
					t.Fatalf("interval=%d, missing calendar days=%v; want %d", tc.interval, gaps, tc.wantGaps)
				}
				if tc.interval == 7 {
					for i, day := range gaps {
						want := start.AddDate(0, 0, i).Format("2006-01-02")
						if day != want {
							t.Fatalf("missing date %d = %s, want %s", i, day, want)
						}
					}
				}
			})
		}
	}
}

// Московская полночь наступает в 21:00 UTC: 3 полных дня до скана
// меняются даже при разнице ровно в одну секунду.
func TestGO293HHWindowAtMoscowMidnight(t *testing.T) {
	before := time.Date(2026, 11, 7, 20, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	_, beforeTo := hhDateWindow(before, 3)
	_, afterTo := hhDateWindow(after, 3)
	if beforeTo == nil || afterTo == nil ||
		*beforeTo != "2026-11-06T23:59:59+03:00" ||
		*afterTo != "2026-11-07T23:59:59+03:00" {
		t.Fatalf("Moscow midnight boundary lost: before=%v, after=%v", beforeTo, afterTo)
	}
}

// Внутридневные сканы при окне полных суток запрашивают одни даты.
func TestGO293HHFourDailyScansUseSameDateWindow(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	midnight := time.Date(2026, time.November, 21, 0, 0, 0, 0, msk)
	for _, hour := range []int{0, 6, 12, 18} {
		from, to := hhDateWindow(midnight.Add(time.Duration(hour)*time.Hour), 3)
		if from == nil || to == nil ||
			*from != "2026-11-18T00:00:00+03:00" ||
			*to != "2026-11-20T23:59:59+03:00" {
			t.Fatalf("hour %d: unexpected date window: %v, %v", hour, from, to)
		}
	}
	from, to := hhDateWindow(midnight.AddDate(0, 0, 1), 3)
	if from == nil || to == nil ||
		*from != "2026-11-19T00:00:00+03:00" ||
		*to != "2026-11-21T23:59:59+03:00" {
		t.Fatalf("date window did not advance at midnight: %v, %v", from, to)
	}
}
