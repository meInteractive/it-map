package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The schedule report is an observation of stored HH source cursors, not a
// simulated scheduler run. It never claims or creates work and never exposes
// company identifiers or source URLs.
var ErrHHScanScheduleStorage = errors.New("HH schedule diagnostics unavailable")

type HHScanScheduleRepository struct {
	DB  *sql.DB
	Now func() time.Time
}

type HHScanScheduleSource struct {
	Enabled        bool
	Favorite       bool
	HasActiveScan  bool
	NextRunAt      int64
	IntervalDays   int
	LastSuccessAt  sql.NullString
}

type HHScanScheduleSummary struct {
	Configured         int64  `json:"configured"`
	Disabled           int64  `json:"disabled"`
	OutsideScope       int64  `json:"outsideScope"`
	ActiveScan         int64  `json:"activeScan"`
	Waiting            int64  `json:"waiting"`
	PotentiallyDue     int64  `json:"potentiallyDue"`
	Recalculated       int64  `json:"recalculated"`
	UnknownLastSuccess int64  `json:"unknownLastSuccess"`
	SuccessfulLast24h  int64  `json:"successfulLast24h"`
	SuccessfulLast48h  int64  `json:"successfulLast48h"`
	SuccessDateUnknown int64  `json:"successDateUnknown"`
	NextDueAt          *int64 `json:"nextDueAt"`
}

type HHScanScheduleReport struct {
	CheckedAt          int64                 `json:"checkedAt"`
	SourceID           string                `json:"sourceId"`
	Enabled            bool                  `json:"enabled"`
	SourceSelected     bool                  `json:"sourceSelected"`
	FavoritesOnly      bool                  `json:"favoritesOnly"`
	IntervalDays       int                   `json:"intervalDays"`
	VacancyAgeDays     int                   `json:"vacancyAgeDays"`
	Summary            HHScanScheduleSummary `json:"summary"`
	PendingHHScans     int64                 `json:"pendingHHScans"`
	RunningHHScans     int64                 `json:"runningHHScans"`
	PendingBreakdown   HHPendingBreakdown    `json:"pendingBreakdown"`
	FinalEligibilityVerified bool            `json:"finalEligibilityVerified"`
}

// Mirrors the interval-change recalculation in scheduleHH without modifying
// stored next_run_at. A missing/invalid success date preserves the stored
// cursor, just as the live scheduler does.
func effectiveHHNextRun(source HHScanScheduleSource, configuredInterval int) (int64, bool, bool) {
	next := source.NextRunAt
	if source.IntervalDays == configuredInterval {
		return next, false, false
	}
	if !source.LastSuccessAt.Valid {
		return next, false, true
	}
	last, err := time.Parse(time.RFC3339Nano, source.LastSuccessAt.String)
	if err != nil || last.UnixMilli() == 0 {
		return next, false, true
	}
	return last.UnixMilli() + int64(configuredInterval)*86400000, true, false
}

func summarizeHHScanSchedule(rows []HHScanScheduleSource, now int64, interval int, favoritesOnly bool) HHScanScheduleSummary {
	out := HHScanScheduleSummary{}
	for _, source := range rows {
		out.Configured++
		if !source.Enabled {
			out.Disabled++
			continue
		}
		if favoritesOnly && !source.Favorite {
			out.OutsideScope++
			continue
		}
		// Последний подтверждённый успех фиксируется только после полного
		// завершения HH cursor. Частичные запуски не обновляют last_success_at.
		if !source.LastSuccessAt.Valid {
			out.SuccessDateUnknown++
		} else if last, err := time.Parse(time.RFC3339Nano, source.LastSuccessAt.String); err != nil || last.UnixMilli() <= 0 || last.UnixMilli() > now {
			out.SuccessDateUnknown++
		} else {
			age := now - last.UnixMilli()
			if age <= int64(48*time.Hour/time.Millisecond) {
				out.SuccessfulLast48h++
				if age <= int64(24*time.Hour/time.Millisecond) {
					out.SuccessfulLast24h++
				}
			}
		}
		if source.HasActiveScan {
			out.ActiveScan++
			continue
		}
		next, recalculated, missing := effectiveHHNextRun(source, interval)
		if recalculated {
			out.Recalculated++
		}
		if missing {
			out.UnknownLastSuccess++
		}
		if next <= now {
			out.PotentiallyDue++
		} else {
			out.Waiting++
			if out.NextDueAt == nil || next < *out.NextDueAt {
				value := next
				out.NextDueAt = &value
			}
		}
	}
	return out
}

func (r HHScanScheduleRepository) ReadHHScanSchedule(ctx context.Context) (json.RawMessage, error) {
	if r.DB == nil {
		return nil, ErrHHScanScheduleStorage
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	defer tx.Rollback()

	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, ErrHHScanScheduleStorage
	}
	stored, err := readSettingsRow(ctx, tx, false)
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	view, err := settingsView(ctx, tx, stored)
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	var settings Settings
	if err = json.Unmarshal(view.PublicJSON(), &settings); err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	if settings.IntervalDays != 1 && settings.IntervalDays != 3 && settings.IntervalDays != 7 {
		return nil, ErrHHScanScheduleStorage
	}
	report := HHScanScheduleReport{
		CheckedAt: now.UnixMilli(),
		SourceID: "hh",
		Enabled: settings.Enabled,
		FavoritesOnly: settings.FavoritesOnly,
		IntervalDays: settings.IntervalDays,
		VacancyAgeDays: settings.VacancyAgeDays,
		FinalEligibilityVerified: false,
	}
	for _, id := range settings.SourceIDs {
		if id == "hh" {
			report.SourceSelected = true
			break
		}
	}
	// All configured HH rows are counted, including excluded/disabled ones.
	// EXISTS avoids multiplying source rows by a user's company-state records
	// or by multiple current-generation jobs.
	rows, err := tx.QueryContext(ctx, `SELECT s.enabled,COALESCE(s.next_run_at,0),s.interval_days,s.last_success_at,
		EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1),
		EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.generation=? AND j.company_id=s.company_id
			AND j.source_id='hh' AND j.type='scan_source' AND j.status IN ('pending','running'))
		FROM vacancy_source_parsing s JOIN company_identity c ON c.id=s.company_id
		WHERE s.source_id='hh'`, settings.OwnerName, generation)
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	sources := []HHScanScheduleSource{}
	for rows.Next() {
		var v HHScanScheduleSource
		var enabled, favorite, active int64
		if err = rows.Scan(&enabled, &v.NextRunAt, &v.IntervalDays, &v.LastSuccessAt, &favorite, &active); err != nil {
			break
		}
		v.Enabled = enabled == 1
		v.Favorite = favorite == 1
		v.HasActiveScan = active == 1
		sources = append(sources, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	report.Summary = summarizeHHScanSchedule(sources, now.UnixMilli(), settings.IntervalDays, settings.FavoritesOnly)

	rows, err = tx.QueryContext(ctx, `SELECT status,COUNT(*) FROM scheduler_jobs
		WHERE generation=? AND type='scan_source' AND source_id='hh'
		AND status IN ('pending','running') GROUP BY status`, generation)
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	for rows.Next() {
		var status string
		var count int64
		if err = rows.Scan(&status, &count); err != nil {
			break
		}
		switch status {
		case "pending":
			report.PendingHHScans = count
		case "running":
			report.RunningHHScans = count
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	report.PendingBreakdown, err = readHHPendingBreakdown(ctx, tx, generation, now.UnixMilli(), settings)
	if err != nil || report.PendingBreakdown.Pending != report.PendingHHScans {
		return nil, ErrHHScanScheduleStorage
	}
	if err = tx.Commit(); err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	body, err := json.Marshal(report)
	if err != nil {
		return nil, ErrHHScanScheduleStorage
	}
	return body, nil
}
