//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/applications"
)

type receiptOutcome struct {
	result applications.CreationResult
	err    error
}

// Hold the real generation row until BOTH repository transactions have reached
// their lock query on distinct server connections. PROCESSLIST exposes the
// current user's own sessions without granting global PROCESS privileges.
// This proves overlapping SQL operations, not merely simultaneous goroutines
// or contention for a single database/sql pool connection.
func (f *receiptFixture) concurrentCreates(inputs [2]applications.CreateInput) [2]receiptOutcome {
	f.t.Helper()
	var repos [2]applications.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(f.t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err := db.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&ids[i]); err != nil {
			f.t.Fatal("cannot identify concurrent writer connection")
		}
		repos[i] = applications.Repository{DB: db, CreationSecret: f.repo.CreationSecret}
	}
	if ids[0] == ids[1] {
		f.t.Fatal("concurrent writers require distinct server connections")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	holder, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		f.t.Fatal("cannot start concurrency barrier transaction")
	}
	const lockQuery = "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE"
	var generation string
	if err := holder.QueryRowContext(ctx, lockQuery).Scan(&generation); err != nil {
		holder.Rollback()
		f.t.Fatal("cannot hold generation barrier")
	}
	var outcomes [2]receiptOutcome
	var wg sync.WaitGroup
	defer func() { cancel(); holder.Rollback(); wg.Wait() }()
	for i := range repos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i].result, outcomes[i].err = repos[i].CreateWithReceipt(ctx, receiptUser, inputs[i])
		}(i)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST
			WHERE ID IN (?,?) AND COMMAND='Query' AND INFO=?`, ids[0], ids[1], lockQuery).Scan(&waiting)
		if err != nil {
			f.t.Fatal("cannot observe both SQL writers at the generation barrier")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			f.t.Fatal("both SQL writers did not reach the generation barrier")
		case <-ticker.C:
		}
	}
	if err := holder.Rollback(); err != nil {
		f.t.Fatal("cannot release concurrency barrier")
	}
	wg.Wait()
	return outcomes
}

func TestMariaDBApplicationConcurrencyDisposable(t *testing.T) {
	for _, scenario := range []string{"same key", "conflicting intent", "same vacancy", "tenth slot"} {
		t.Run(scenario, func(t *testing.T) {
			f := openReceiptFixture(t)
			inputs := [2]applications.CreateInput{f.input, f.input}
			var wantError error
			wantApps := 1
			switch scenario {
			case "conflicting intent":
				inputs[1].Comment = "CI synthetic different intent"
				wantError = applications.ErrIntentConflict
			case "same vacancy":
				vacancy := int64(1)
				inputs[0].VacancyID, inputs[1].VacancyID = &vacancy, &vacancy
				inputs[1].IdempotencyKey = receiptKey(2)
				wantError = applications.ErrDuplicateVacancy
			case "tenth slot":
				for i := 0; i < 9; i++ {
					seed := f.input
					seed.IdempotencyKey = receiptKey(20 + i)
					if _, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, seed); err != nil {
						t.Fatalf("cannot prepare nine synthetic applications: %T", err)
					}
				}
				f.assertCounts(9, 9, 9)
				inputs[1].IdempotencyKey = receiptKey(2)
				wantApps, wantError = 10, applications.ErrApplicationLimit
			}
			outcomes := f.concurrentCreates(inputs)
			fresh, replayed, rejected := 0, 0, 0
			var winnerID int64
			for i, out := range outcomes {
				if out.err != nil {
					if wantError == nil || !errors.Is(out.err, wantError) {
						t.Fatalf("unexpected concurrent creation error: %T", out.err)
					}
					rejected++
					continue
				}
				if out.result.Replayed {
					replayed++
				} else {
					fresh++
				}
				if out.result.CreatedApplicationID <= 1 || len(out.result.Records) != wantApps {
					t.Fatal("concurrent result lost durable identity or company response")
				}
				if winnerID != 0 && winnerID != out.result.CreatedApplicationID {
					t.Fatal("same intent returned different durable identities")
				}
				winnerID = out.result.CreatedApplicationID
				// Replay each successful intent after both transactions finished.
				retry, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, inputs[i])
				if err != nil || !retry.Replayed || retry.CreatedApplicationID != winnerID {
					t.Fatalf("concurrent winner cannot be replayed: %T", err)
				}
				row, err := f.repo.ByID(f.ctx, receiptUser, winnerID)
				if err != nil || row.Comment != inputs[i].Comment || len(row.History) != 1 {
					t.Fatal("concurrent winner's fields/history were overwritten")
				}
			}
			if fresh != 1 || (wantError == nil && (replayed != 1 || rejected != 0)) ||
				(wantError != nil && (replayed != 0 || rejected != 1)) {
				t.Fatal("concurrent operations did not produce exactly one fresh winner")
			}
			f.assertCounts(wantApps, wantApps, wantApps)
			if f.count(`SELECT COUNT(*) FROM application_creation_receipts r
				LEFT JOIN company_applications a ON a.id=r.created_application_id AND a.user_name=r.user_name
				WHERE r.user_name=? AND a.id IS NULL`, receiptUser) != 0 {
				t.Fatal("concurrent creation left incomplete/orphan receipts")
			}
		})
	}
}
