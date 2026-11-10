package parser

import (
	"encoding/json"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

func TestGO304PublicationWindowMatchesCatalogWeek(t *testing.T) {
	// 10 ноября в Москве; календарная «Неделя» начинается 4 ноября.
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	daily := make([]funnelDay, 30)
	for i := range daily {
		daily[i].Day = now.In(moscow).AddDate(0, 0, i-29).Format("2006-01-02")
	}
	report := newFunnelPublicationWindow(now, daily)
	if report.AsOf != "2026-11-10" || report.WeekFrom != "2026-11-04" ||
		len(report.Days) != 7 || report.Days[6].Day != "2026-11-10" {
		t.Fatalf("неверный календарный интервал: %+v", report)
	}
	cases := []struct {
		name string
		date vacancies.Dates
		want string
	}{
		{"начало недели", vacancies.Dates{PublishedAt: "2026-11-04"}, "inWeek"},
		{"текущий день", vacancies.Dates{PublishedAt: "2026-11-10"}, "inWeek"},
		{"раньше недели", vacancies.Dates{PublishedAt: "2026-11-03", CreatedAt: "2026-11-10"}, "older"},
		{"будущее", vacancies.Dates{PublishedAt: "2026-11-11", LoadedAt: "2026-11-10"}, "future"},
		{"резерв загружено", vacancies.Dates{PublishedAt: "invalid", LoadedAt: "2026-11-09"}, "inWeek"},
		{"резерв создано", vacancies.Dates{CreatedAt: "2026-11-09T23:55:00Z"}, "inWeek"},
		{"даты отсутствуют", vacancies.Dates{}, "undated"},
		{"некорректная дата", vacancies.Dates{PublishedAt: "2026-02-30"}, "undated"},
	}
	for _, tc := range cases {
		var point funnelPublicationWindowDay
		point.add(tc.date, report.WeekFrom, report.AsOf)
		if point.Total != 1 || point.InWeek+point.Older+point.Future+point.Undated != 1 {
			t.Fatalf("%s: категории не образуют разбиение: %+v", tc.name, point)
		}
		expected := tc.want == "inWeek"
		if vacancies.MatchesPublication(vacancies.Week, tc.date, now) != expected {
			t.Fatalf("%s: расхождение с реальным фильтром каталога", tc.name)
		}
	}
	point := &report.Days[6]
	for _, tc := range cases {
		point.add(tc.date, report.WeekFrom, report.AsOf)
	}
	if point.Total != 8 || point.InWeek != 4 || point.Older != 1 ||
		point.Future != 1 || point.Undated != 2 {
		t.Fatalf("общее число первых CREATE должно равняться сумме групп: %+v", point)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Days []struct {
			Day string `json:"day"`
			InWeek int64 `json:"inWeek"`
		} `json:"days"`
	}
	if err = json.Unmarshal(raw, &decoded); err != nil || decoded.Days[6].InWeek != 4 {
		t.Fatalf("JSON не сохраняет агрегаты разбивки: %s %v", raw, err)
	}
}

func TestGO304WeekWindowAcrossMoscowMidnight(t *testing.T) {
	now := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC)
	report := newFunnelPublicationWindow(now, nil)
	if report.AsOf != "2026-11-10" || report.WeekFrom != "2026-11-04" ||
		len(report.Days) != 0 {
		t.Fatalf("граница московских суток потеряна: %+v", report)
	}
}
