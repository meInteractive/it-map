package parser

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"itmap/internal/apifyruns"
)

// JSON1 uses the first duplicate; the surrounding settings parser uses the last.
// A JSON string "1" does not compare equal to SQL integer 1.
func claimAgenciesEnabled(raw []byte) (bool, error) {
	value, err := jsonPathFirst(raw, []string{"agenciesEnabled"})
	if err != nil {
		return false, err
	}
	value = bytes.TrimSpace(value)
	if len(value) == 0 || value[0] == '"' {
		return false, nil
	}
	text, _, err := sqliteJSONText(raw, "agenciesEnabled")
	return text == "1" || text == "1.0", err
}

// Один новый HH Actor в 15 минут; GET ранее сохранённого Actor не ограничен этим интервалом.
const hhActorLaunchCooldown = 15 * time.Minute

type claimView struct {
	busy           map[int64]bool
	pages          map[string]string
	operations     map[string]bool
	verifiedHH     map[hhCandidateKey]bool
	blockedHHScans  map[string]bool
	goOperationKeys map[string]string
	orphanPaidScans map[string]bool
	// A reserved operation without provider Run ID cannot be safely reconciled.
	// Keep it fenced in both HH phases; do not issue a second paid POST.
	unconfirmedRunIDs map[string]bool
	lastListingStart int64
}

func claimTypes(requested []string) (map[string]bool, error) {
	available := DefaultSettings().Priorities
	out := map[string]bool{}
	if requested == nil {
		for kind := range available {
			out[kind] = true
		}
		return out, nil
	}
	for _, kind := range requested {
		if _, ok := available[kind]; !ok {
			return nil, ErrJobInput
		}
		out[kind] = true
	}
	return out, nil
}
func readClaimView(ctx context.Context, tx *sql.Tx, generation string) (claimView, error) {
	view := claimView{busy: map[int64]bool{}, pages: map[string]string{}, operations: map[string]bool{}, goOperationKeys: map[string]string{}, orphanPaidScans: map[string]bool{}, unconfirmedRunIDs: map[string]bool{}}
	operationCreated := map[string]int64{}
	rows, e := tx.QueryContext(ctx, "SELECT candidate_id FROM scheduler_jobs WHERE generation=? AND status='running' AND candidate_id IS NOT NULL", generation)
	if e != nil {
		return view, e
	}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return view, e
		}
		view.busy[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return view, e
	}
	rows, e = tx.QueryContext(ctx, "SELECT operation_key,created_at,run_id FROM apify_runs WHERE generation=?", generation)
	if e != nil {
		return view, e
	}
	for rows.Next() {
		var key string
		var created int64
		var runID sql.NullString
		if e = rows.Scan(&key, &created, &runID); e != nil {
			rows.Close()
			return view, e
		}
		view.operations[key] = true
		operationCreated[key] = created
		if !runID.Valid || strings.TrimSpace(runID.String) == "" {
			view.unconfirmedRunIDs[key] = true
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return view, e
	}
	rows, e = tx.QueryContext(ctx, "SELECT id,result_json FROM vacancy_discovery_runs WHERE generation=?", generation)
	if e != nil {
		return view, e
	}
	for rows.Next() {
		var id string
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return view, e
		}
		if len(raw) == 0 {
			raw = []byte("null")
		}
		page, present, err := sqliteJSONText(raw, "paused", "apify", "page")
		if err != nil {
			rows.Close()
			return view, err
		}
		if !present {
			page = "0"
		}
		if strings.HasPrefix(id, "go399:") {
			var c hhListingCursor
			if json.Unmarshal(raw, &c) == nil && c.Version == 1 && c.Page >= 0 && c.Page < 100 {
				page = strconv.FormatInt(c.Page, 10)
				// Ключ в apify_runs — go398:SHA(input), а не строка listing:...
				for _, n := range []int64{c.Page, c.Page - 1} {
					if n < 0 {
						continue
					}
					options := c.options()
					options.Page = n
					key, keyErr := apifyruns.HHOperationIdentity(c.Operation, fmt.Sprintf("listing:%s:%d", id, n), options)
					if keyErr != nil {
						continue
					}
					if n == c.Page {
						scanID := strings.TrimPrefix(id, "go399:")
						view.goOperationKeys[scanID] = key
						if (c.RunID != "" || c.DatasetID != "") && !view.operations[key] {
							// Cursor помнит платный Actor, но ledger не подтверждает
							// операцию. Не выпускать повторный POST.
							view.orphanPaidScans[scanID] = true
						}
					}
					if created := operationCreated[key]; created > view.lastListingStart {
						view.lastListingStart = created
					}
				}
			}
		}
		view.pages[id] = page
	}
	e = rows.Err()
	rows.Close()
	return view, e
}
func claimEligible(job Job, now int64, settings Settings, options ClaimOptions, types map[string]bool, view claimView) (bool, bool, error) {
	if job.Status != "pending" || job.RunAfter > now || !types[job.Type] || job.AgencyID != nil && !settings.AgenciesEnabled {
		return false, false, nil
	}
	if options.JobID != nil && job.ID != *options.JobID || options.Diagnostic && job.Type != "diagnostic" || job.CandidateID != nil && view.busy[*job.CandidateID] {
		return false, false, nil
	}
	if options.SourceID != nil && (job.SourceID == nil || *job.SourceID != *options.SourceID) {
		return false, false, nil
	}
	hh := job.SourceID != nil && *job.SourceID == "hh"
	if options.VerifiedHHOnly && hh && job.Type == "scan_source" && view.blockedHHScans[job.ID] {
		return false, false, nil
	}
	if options.VerifiedHHOnly && hh && job.CandidateID != nil && (job.CompanyID == nil || !view.verifiedHH[hhCandidateKey{*job.CompanyID, *job.CandidateID}]) {
		return false, false, nil
	}
	if options.DeferredApifyOnly && (!hh || job.Type != "scan_source" || (job.ReasonCode != "apify_busy" && job.ReasonCode != "apify_budget") || job.RunAfter > now-300000) {
		return false, false, nil
	}
	if options.AllowApify != nil && !*options.AllowApify && hh && (job.Type == "scan_source" || job.Type == "fetch_full_text") {
		return false, false, nil
	}
	page := false
	if hh && job.Type == "scan_source" {
		run, present, e := sqliteJSONText(job.Payload, "runId")
		if e != nil {
			return false, false, e
		}
		if !present {
			run = job.ID
		}
		cursor, ok := view.pages[run]
		if !ok {
			cursor = "0"
		}
		page = view.operations["listing:"+run+":"+cursor]
		if options.HHScanPhase != "" {
			// В Go scan cursor привязан к go399:job.ID, а не к JS runId.
			// Разделение выполняется по оплаченному operation_key,
			// чтобы GET существующего Actor не считался новым POST.
			key, known := view.goOperationKeys[job.ID]
			if !known {
				// Курсор не подтверждён либо ещё не создан: новому запуску
				// разрешён только verified HH scan без старого paid cursor.
				if view.pages["go399:"+job.ID] != "" {
					return false, false, nil
				}
			}
			// The ledger can contain starting/uncertain with no provider
			// Run ID. GET cannot reconcile it, while a fresh paid POST is
			// unsafe. Never claim that job in either automated HH phase.
			if known && view.operations[key] && view.unconfirmedRunIDs[key] {
				return false, false, nil
			}
			page = known && view.operations[key]
			switch options.HHScanPhase {
			case "launch":
				if page || view.orphanPaidScans[job.ID] || view.lastListingStart > now-int64(hhActorLaunchCooldown/time.Millisecond) {
					return false, page, nil
				}
			case "reconcile":
				if !page {
					return false, page, nil
				}
			default:
				return false, false, ErrJobInput
			}
		}
		if options.AllowApifyStarts != nil && !*options.AllowApifyStarts && !page {
			return false, false, nil
		}
	}
	return true, page, nil
}

type jobScore struct {
	integer  int64
	real     float64
	floating bool
}

func score(job Job, now int64) jobScore {
	difference := now - job.CreatedAt
	age := difference / 3600000
	if (now^job.CreatedAt)&(now^difference) < 0 {
		// SQLite promotes only overflowing subtraction to REAL; ordinary integer
		// division must retain low bits near an hour boundary above 2^53.
		age = int64(math.Trunc((float64(now) - float64(job.CreatedAt)) / 3600000))
	}
	total := job.Priority + age
	s := jobScore{integer: total}
	if (job.Priority^total)&(age^total) < 0 {
		s.floating = true
		s.real = float64(job.Priority) + float64(age)
	}
	if job.SourceID != nil && *job.SourceID == "hh" && job.Type == "scan_source" {
		if s.floating {
			s.real -= 50
		} else if s.integer < math.MinInt64+50 {
			s.floating = true
			s.real = float64(s.integer) - 50
		} else {
			s.integer -= 50
		}
	}
	return s
}
func scoreCompare(a, b jobScore) int {
	if !a.floating && !b.floating {
		if a.integer < b.integer {
			return -1
		}
		if a.integer > b.integer {
			return 1
		}
		return 0
	}
	if a.floating && b.floating {
		if a.real < b.real {
			return -1
		}
		if a.real > b.real {
			return 1
		}
		return 0
	}
	if a.floating {
		return -scoreCompare(b, a)
	}
	if b.real >= 9223372036854775808.0 {
		return -1
	}
	if b.real < -9223372036854775808.0 {
		return 1
	}
	integer := int64(b.real)
	if a.integer < integer {
		return -1
	}
	if a.integer > integer {
		return 1
	}
	if float64(a.integer) < b.real {
		return -1
	}
	if float64(a.integer) > b.real {
		return 1
	}
	return 0
}
func claimBefore(a Job, aPage bool, b Job, bPage bool, now int64) bool {
	if compared := scoreCompare(score(a, now), score(b, now)); compared != 0 {
		return compared > 0
	}
	if aPage != bPage {
		return aPage
	}
	if a.RunAfter != b.RunAfter {
		return a.RunAfter < b.RunAfter
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt < b.CreatedAt
	}
	return strings.Compare(a.ID, b.ID) < 0
}
