package parser

import (
	"database/sql"
	"testing"
	"time"
)

func TestGO303ConfirmedHHSuccessCoverage24h48h(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC).UnixMilli()
	at := func(ms int64) sql.NullString {
		return sql.NullString{Valid: true, String: time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)}
	}
	row := func(last sql.NullString) HHScanScheduleSource {
		return HHScanScheduleSource{Enabled: true, Favorite: true, IntervalDays: 1, NextRunAt: now + 1000, LastSuccessAt: last}
	}
	rows := []HHScanScheduleSource{
		row(at(now)),                                    // Сейчас.
		row(at(now - int64(24*time.Hour/time.Millisecond))), // Ровно 24 часа назад включительно.
		row(at(now - int64(24*time.Hour/time.Millisecond) - 1)), // Уже только 48-часовой интервал.
		row(at(now - int64(48*time.Hour/time.Millisecond))), // Ровно 48 часов назад.
		row(at(now - int64(48*time.Hour/time.Millisecond) - 1)), // Старше двух суток.
		row(sql.NullString{}),                           // Ещё не было подтверждённого успеха.
		row(sql.NullString{Valid: true, String: "bad"}), // Некорректная дата.
		row(at(now + 1)),                                // Будущая дата не подтверждает успешный сбор.
		{Enabled: false, Favorite: true, LastSuccessAt: at(now)}, // Отключено.
		{Enabled: true, Favorite: false, LastSuccessAt: at(now)}, // Вне области избранного.
	}
	rows[0].HasActiveScan = true // Текущий pending не скрывает предыдущий успех.
	out := summarizeHHScanSchedule(rows, now, 1, true)
	if out.Configured != 10 || out.Disabled != 1 || out.OutsideScope != 1 ||
		out.ActiveScan != 1 || out.SuccessfulLast24h != 2 ||
		out.SuccessfulLast48h != 4 || out.SuccessDateUnknown != 3 {
		t.Fatalf("неверный охват успешно проверенных HH-источников: %+v", out)
	}
	// При отключении избранного включённый источник учитывается,
	// но отключённый источник не увеличивает охват.
	all := summarizeHHScanSchedule(rows, now, 1, false)
	if all.SuccessfulLast24h != 3 || all.SuccessfulLast48h != 5 ||
		all.SuccessDateUnknown != 3 {
		t.Fatalf("неверная область охвата без фильтра избранного: %+v", all)
	}
}

func TestGO303NoSourcesHaveZeroRecentSuccesses(t *testing.T) {
	out := summarizeHHScanSchedule(nil, time.Now().UnixMilli(), 1, true)
	if out.SuccessfulLast24h != 0 || out.SuccessfulLast48h != 0 ||
		out.SuccessDateUnknown != 0 {
		t.Fatalf("пустая область должна давать нулевой охват: %+v", out)
	}
}
