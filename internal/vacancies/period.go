// Package vacancies holds the legacy API's read-side vacancy projections.
package vacancies

import (
	"errors"
	"strings"
	"time"
)

// Period is the v316 publication-date filter used by /api/vacancies/list.
type Period string

const (
	Week      Period = "week"
	Month     Period = "month"
	TwoMonths Period = "twoMonths"
	Archive   Period = "archive"
	All       Period = "all"
)

func ParsePeriod(v string) (Period, error) {
	if v == "" {
		return Week, nil
	}
	switch Period(v) {
	case Week, Month, TwoMonths, Archive, All:
		return Period(v), nil
	}
	return "", errors.New("invalid publication period")
}

// PublicationDate accepts a valid YYYY-MM-DD prefix (the legacy created_at
// column may contain a timestamp). Match source ECMAScript trim and Date.UTC's
// 0..99 year remapping: those four-digit years fail its subsequent year check.
func PublicationDate(raw string) (time.Time, bool) {
	s := strings.TrimFunc(raw, func(r rune) bool {
		return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 ||
			r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 ||
			r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
	if len(s) < 10 {
		return time.Time{}, false
	}
	s = s[:10]
	for i, r := range s {
		if i == 4 || i == 7 {
			if r != '-' {
				return time.Time{}, false
			}
			continue
		}
		if r < '0' || r > '9' {
			return time.Time{}, false
		}
	}
	d, err := time.Parse("2006-01-02", s)
	return d, err == nil && d.Year() >= 100 && d.Format("2006-01-02") == s
}

type Dates struct{ PublishedAt, LoadedAt, CreatedAt string }

func (d Dates) effective() (time.Time, bool) {
	for _, candidate := range []string{d.PublishedAt, d.LoadedAt, d.CreatedAt} {
		if t, ok := PublicationDate(candidate); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// MatchesPublication replicates shared/publication-period.ts: Moscow calendar,
// inclusive current day, month-end clamping and a strict two-month archive.
func MatchesPublication(p Period, dates Dates, now time.Time) bool {
	if p == All {
		return true
	}
	date, ok := dates.effective()
	if !ok {
		return false
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return false
	} // never substitute server-local time
	moscow := now.In(loc)
	today := time.Date(moscow.Year(), moscow.Month(), moscow.Day(), 0, 0, 0, 0, time.UTC)
	var from time.Time
	switch p {
	case Week:
		from = today.AddDate(0, 0, -6)
	case Month:
		from = monthBefore(today, 1)
	case TwoMonths, Archive:
		from = monthBefore(today, 2)
	default:
		return false
	}
	if p == Archive {
		return date.Before(from)
	}
	return !date.Before(from) && !date.After(today)
}

func monthBefore(today time.Time, n int) time.Time {
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -n, 0)
	last := first.AddDate(0, 1, -1).Day()
	day := today.Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)
}
