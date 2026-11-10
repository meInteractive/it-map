package parser

import (
	"context"
	"database/sql"
	"time"
)

// HHPendingBreakdown описывает ожидающие HH-scan без идентификаторов,
// payload, URL или платных вызовов. Категории взаимоисключающие.
type HHPendingBreakdown struct {
	Pending             int64  `json:"pending"`
	NotDue              int64  `json:"notDue"`
	UnverifiedCursor    int64  `json:"unverifiedCursor"`
	OrphanPaid          int64  `json:"orphanPaid"`
	ReconcileCandidate  int64  `json:"reconcileCandidate"`
	LaunchCooldown      int64  `json:"launchCooldown"`
	LaunchCandidate     int64  `json:"launchCandidate"`
	Other               int64  `json:"other"`
	OldestPendingAt     *int64 `json:"oldestPendingAt"`
}

// Это предварительная оценка доступности на момент снимка, а не пробный claim:
// бюджеты, lease, лимиты цикла и разрешение нового Actor здесь не проверяются.
func summarizeHHPending(jobs []Job, now int64, settings Settings, view claimView) HHPendingBreakdown {
	var out HHPendingBreakdown
	source := "hh"
	types := map[string]bool{"scan_source": true}
	options := ClaimOptions{SourceID: &source, VerifiedHHOnly: true}
	for _, job := range jobs {
		if job.Type != "scan_source" || job.Status != "pending" || job.SourceID == nil || *job.SourceID != "hh" {
			continue
		}
		out.Pending++
		if out.OldestPendingAt == nil || job.CreatedAt < *out.OldestPendingAt {
			value := job.CreatedAt
			out.OldestPendingAt = &value
		}
		switch {
		case job.RunAfter > now:
			out.NotDue++
			continue
		case view.blockedHHScans[job.ID]:
			out.UnverifiedCursor++
			continue
		case view.orphanPaidScans[job.ID]:
			out.OrphanPaid++
			continue
		}
		// Существующий курсор без подтверждённого operation_key нельзя
		// принимать за разрешение на новый платный запуск.
		if _, known := view.goOperationKeys[job.ID]; !known && view.pages["go399:"+job.ID] != "" {
			out.UnverifiedCursor++
			continue
		}
		options.HHScanPhase = "reconcile"
		reconcile, _, reconcileErr := claimEligible(job, now, settings, options, types, view)
		options.HHScanPhase = "launch"
		launch, _, launchErr := claimEligible(job, now, settings, options, types, view)
		switch {
		case reconcileErr != nil || launchErr != nil:
			out.Other++
		case reconcile:
			out.ReconcileCandidate++
		case launch:
			out.LaunchCandidate++
		case view.lastListingStart > now-int64(hhActorLaunchCooldown/time.Millisecond):
			out.LaunchCooldown++
		default:
			out.Other++
		}
	}
	return out
}

// readHHPendingBreakdown повторно использует правила claimEligible, не выдавая
// lease и не изменяя таблицы. Сведения о конкретных компаниях не возвращаются.
func readHHPendingBreakdown(ctx context.Context, tx *sql.Tx, generation string, now int64, settings Settings) (HHPendingBreakdown, error) {
	var empty HHPendingBreakdown
	view, err := readClaimView(ctx, tx, generation)
	if err != nil {
		return empty, err
	}
	_, blocked, err := readVerifiedHHLineage(ctx, tx, generation)
	if err != nil {
		return empty, err
	}
	view.blockedHHScans = blocked
	rows, err := tx.QueryContext(ctx, "SELECT "+jobColumns+" FROM scheduler_jobs WHERE generation=? AND status='pending' AND type='scan_source' AND source_id='hh'", generation)
	if err != nil {
		return empty, err
	}
	jobs := []Job{}
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			rows.Close()
			return empty, scanErr
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	return summarizeHHPending(jobs, now, settings, view), nil
}
