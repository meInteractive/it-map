package parser

import (
	"time"

	"itmap/internal/vacancies"
)

// funnelPublicationWindow сопоставляет уникальные первые CREATE за последние
// семь дней импорта с текущим фильтром каталога «Неделя». Категории не обещают,
// что запись видна в UI: там действуют дополнительные фильтры и права.
type funnelPublicationWindow struct {
	AsOf     string                     `json:"asOf"`
	WeekFrom string                     `json:"weekFrom"`
	Days     []funnelPublicationWindowDay `json:"days"`
}

type funnelPublicationWindowDay struct {
	Day     string `json:"day"`
	Total   int64  `json:"total"`
	InWeek  int64  `json:"inWeek"`
	Older   int64  `json:"older"`
	Future  int64  `json:"future"`
	Undated int64  `json:"undated"`
}

func newFunnelPublicationWindow(now time.Time, daily []funnelDay) funnelPublicationWindow {
	local := now.In(moscow)
	today := local.Format("2006-01-02")
	todayDate, _ := time.Parse("2006-01-02", today)
	out := funnelPublicationWindow{
		AsOf: today, WeekFrom: todayDate.AddDate(0, 0, -6).Format("2006-01-02"),
		Days: make([]funnelPublicationWindowDay, 0, 7),
	}
	start := len(daily) - 7
	if start < 0 {
		start = 0
	}
	for _, item := range daily[start:] {
		out.Days = append(out.Days, funnelPublicationWindowDay{Day: item.Day})
	}
	return out
}

// add использует тот же приоритет PublishedAt → LoadedAt → CreatedAt,
// что и effectivePublicationDate в каталоге и vacancies.MatchesPublication.
func (d *funnelPublicationWindowDay) add(dates vacancies.Dates, weekFrom, today string) {
	d.Total++
	var effective time.Time
	found := false
	for _, raw := range []string{dates.PublishedAt, dates.LoadedAt, dates.CreatedAt} {
		if parsed, ok := vacancies.PublicationDate(raw); ok {
			effective, found = parsed, true
			break
		}
	}
	if !found {
		d.Undated++
		return
	}
	switch day := effective.Format("2006-01-02"); {
	case day < weekFrom:
		d.Older++
	case day > today:
		d.Future++
	default:
		d.InWeek++
	}
}
