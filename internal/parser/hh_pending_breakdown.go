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
	UnverifiedLineage   int64  `json:"unverifiedLineage"`
	UnverifiedSourceChanged int64 `json:"unverifiedSourceChanged"`
	UnverifiedLegacyFilters  int64 `json:"unverifiedLegacyFilters"`
	UnverifiedMalformedCursor int64 `json:"unverifiedMalformedCursor"`
	UnverifiedOtherProof int64 `json:"unverifiedOtherProof"`
	MissingOperation    int64  `json:"missingOperation"`
	OrphanPaid          int64  `json:"orphanPaid"`
	ReconcileCandidate  int64  `json:"reconcileCandidate"`
	LaunchCooldown      int64  `json:"launchCooldown"`
	LaunchCandidate     int64  `json:"launchCandidate"`
	Other               int64  `json:"other"`
	UnconfirmedRunID    int64  `json:"unconfirmedRunId"`
	OldestUnconfirmedAt *int64 `json:"oldestUnconfirmedAt"`
	OldestPendingAt     *int64 `json:"oldestPendingAt"`
	OldestUnverifiedAt  *int64 `json:"oldestUnverifiedAt"`
	OldestReconcileAt   *int64 `json:"oldestReconcileAt"`
	OldestCooldownAt    *int64 `json:"oldestCooldownAt"`
}

// olderHHStamp сохраняет самое раннее время внутри агрегата без ID заданий.
func olderHHStamp(dest **int64, at int64) {
	if *dest == nil || at < **dest {
		value := at
		*dest = &value
	}
}

// Это предварительная оценка доступности на момент снимка, а не пробный claim:
// бюджеты, lease, лимиты цикла и разрешение нового Actor здесь не проверяются.
func summarizeHHPending(jobs []Job, now int64, settings Settings, view claimView) HHPendingBreakdown {
	return summarizeHHPendingWithReasons(jobs, now, settings, view, nil)
}

func summarizeHHPendingWithReasons(jobs []Job, now int64, settings Settings, view claimView, reasons map[string]string) HHPendingBreakdown {
	var out HHPendingBreakdown
	source := "hh"
	types := map[string]bool{"scan_source": true}
	options := ClaimOptions{SourceID: &source, VerifiedHHOnly: true}
	for _, job := range jobs {
		if job.Type != "scan_source" || job.Status != "pending" || job.SourceID == nil || *job.SourceID != "hh" {
			continue
		}
		out.Pending++
		olderHHStamp(&out.OldestPendingAt, job.CreatedAt)
		switch {
		case job.RunAfter > now:
			out.NotDue++
			continue
		case view.blockedHHScans[job.ID]:
			out.UnverifiedCursor++
			out.UnverifiedLineage++
			switch reasons[job.ID] {
			case hhBlockedSourceChanged:
				out.UnverifiedSourceChanged++
			case hhBlockedLegacyFilters:
				out.UnverifiedLegacyFilters++
			case hhBlockedMalformedCursor:
				out.UnverifiedMalformedCursor++
			default:
				out.UnverifiedOtherProof++
			}
			olderHHStamp(&out.OldestUnverifiedAt, job.CreatedAt)
			continue
		case view.orphanPaidScans[job.ID]:
			out.OrphanPaid++
			continue
		}
		// Существующий курсор без подтверждённого operation_key нельзя
		// принимать за разрешение на новый платный запуск.
		if _, known := view.goOperationKeys[job.ID]; !known && view.pages["go399:"+job.ID] != "" {
			out.UnverifiedCursor++
			out.MissingOperation++
			olderHHStamp(&out.OldestUnverifiedAt, job.CreatedAt)
			continue
		}
		// Запись оплаченной операции без Run ID не подходит для GET
		// и не даёт разрешения на следующий платный запуск.
		if key, known := view.goOperationKeys[job.ID]; known && view.operations[key] && view.unconfirmedRunIDs[key] {
			out.Other++
			out.UnconfirmedRunID++
			olderHHStamp(&out.OldestUnconfirmedAt, job.CreatedAt)
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
			olderHHStamp(&out.OldestReconcileAt, job.CreatedAt)
		case launch:
			out.LaunchCandidate++
		case view.lastListingStart > now-int64(hhActorLaunchCooldown/time.Millisecond):
			out.LaunchCooldown++
			olderHHStamp(&out.OldestCooldownAt, job.CreatedAt)
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
	_, blocked, reasons, err := readVerifiedHHLineageReasons(ctx, tx, generation)
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
	return summarizeHHPendingWithReasons(jobs, now, settings, view, reasons), nil
}
