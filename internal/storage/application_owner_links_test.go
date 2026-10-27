//go:build mariadb && vacancycreates && vacancyedits && vacancydeletes && v39restore && restorehttp && applicationlinks

package storage

import (
	"context"
	"database/sql"
	"errors"
	"itmap/internal/applications"
	"testing"
	"time"
)

func TestMariaDBApplicationOwnerLinkDisposable(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(map[bool]string{true: "create before owner move", false: "owner move before create"}[first], func(t *testing.T) { runAppLinkMutation(t, false, first, false) })
	}
	t.Run("late receipt fault rolls back application history and preserves another user", func(t *testing.T) {
		f, repo, id, input := openAppLinkFixture(t)
		fault, db := openMutationFaultPool(t, "INSERT INTO application_creation_receipts")
		bad := repo
		bad.DB = db
		_, e := bad.CreateWithReceipt(f.ctx, "Demo", input)
		fault.mu.Lock()
		fired, writes, commits := fault.fired, fault.writes, fault.commits
		fault.mu.Unlock()
		if !errors.Is(e, errMutationFault) || !fired || writes != 1 || commits != 0 {
			t.Fatal("receipt fault was not after actual SQL before commit")
		}
		if f.count("SELECT COUNT(*) FROM company_applications WHERE user_name='Demo' AND vacancy_id=?", id) != 0 || f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", appLinkKey) != 0 || f.count("SELECT COUNT(*) FROM company_applications WHERE user_name='Nastya' AND vacancy_id=?", id) != 1 || f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.vacancy_id=?", id) != 1 {
			t.Fatal("late failed intent left partial data or changed another owner")
		}
		db.Close()
		out, e := repo.CreateWithReceipt(f.ctx, "Demo", input)
		if e != nil || out.Replayed {
			t.Fatal("fresh retry failed after complete intent rollback")
		}
	})
}
func TestMariaDBApplicationVacancyDeleteDisposable(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(map[bool]string{true: "create before hard detach delete", false: "hard detach delete before create"}[first], func(t *testing.T) { runAppLinkMutation(t, true, first, false) })
	}
	t.Run("application delete leaves other owner's moved links and dead intent", func(t *testing.T) {
		f, repo, id, input := openAppLinkFixture(t)
		out, e := repo.CreateWithReceipt(f.ctx, "Demo", input)
		if e != nil {
			t.Fatal("cannot create linked delete intent")
		}
		edit := f.editInput(f.detail("Eugene", id))
		edit.CompanyName = crudMoveCompany
		if _, e = f.repo.UpdateManual(f.ctx, "Eugene", id, edit); e != nil {
			t.Fatal("cannot move linked vacancy")
		}
		if owner, e := repo.Delete(f.ctx, "Demo", out.CreatedApplicationID); e != nil || owner != crudMoveCompany {
			t.Fatal("application delete lost current moved owner")
		}
		if _, e = repo.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrApplicationDeleted) {
			t.Fatal("deleted moved application resurrected from receipt")
		}
		if f.count("SELECT COUNT(*) FROM company_applications WHERE user_name='Nastya' AND vacancy_id=? AND company_name=?", id, crudMoveCompany) != 1 || f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name='Nastya' AND vacancy_id=? AND company_name=?", id, crudMoveCompany) != 1 || f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name='Nastya' AND a.vacancy_id=?", id) != 1 {
			t.Fatal("application deletion removed another user's moved history/tasks")
		}
	})
}

// The second actor may complete while the first is paused after SQL. Either
// actual lock waiting or completed overlapping work is a valid serial ordering.
func appLinkWaitOrDone(t *testing.T, ctx context.Context, db *sql.DB, ch chan appLinkOutcome, prefix string) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case out := <-ch:
			ch <- out
			return
		default:
		}
		var n int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND INFO LIKE ?", prefix+"%").Scan(&n); e != nil {
			t.Fatal("cannot observe overlapping application deletion")
		}
		if n > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("second application deletion actor neither waited nor completed")
		case <-ctx.Done():
			t.Fatal("application deletion overlap timed out")
		case <-tick.C:
		}
	}
}
func TestMariaDBApplicationDeleteOwnerRaceDisposable(t *testing.T) {
	for _, deleteFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "delete first", false: "owner move first"}[deleteFirst], func(t *testing.T) {
			f, repo, id, input := openAppLinkFixture(t)
			created, e := repo.CreateWithReceipt(f.ctx, "Demo", input)
			if e != nil {
				t.Fatal("cannot seed delete/owner intent")
			}
			edit := f.editInput(f.detail("Eugene", id))
			edit.CompanyName = crudMoveCompany
			prefix := "DELETE FROM company_applications"
			if !deleteFirst {
				prefix = "UPDATE company_applications SET company_name="
			}
			fault, db, release := appLinkBarrier(t, prefix)
			ar, vr := repo, f.repo
			ar.DB, vr.DB = db, db
			deleted, moved := make(chan appLinkOutcome, 1), make(chan appLinkOutcome, 1)
			startDelete := func() {
				go func() {
					_, e := ar.Delete(f.ctx, "Demo", created.CreatedApplicationID)
					deleted <- appLinkOutcome{err: e}
				}()
			}
			startMove := func() {
				go func() { _, e := vr.UpdateManual(f.ctx, "Eugene", id, edit); moved <- appLinkOutcome{err: e} }()
			}
			if deleteFirst {
				startDelete()
			} else {
				startMove()
			}
			appLinkEntered(t, fault.entered)
			if deleteFirst {
				startMove()
				appLinkWaitOrDone(t, f.ctx, f.db, moved, "UPDATE company_applications SET company_name=")
			} else {
				startDelete()
				appLinkWaitOrDone(t, f.ctx, f.db, deleted, "SELECT company_name FROM company_applications")
			}
			release()
			d, m := appLinkDone(t, deleted), appLinkDone(t, moved)
			if d.err != nil || m.err != nil {
				t.Fatalf("application delete/owner move failed: %T / %T", d.err, m.err)
			}
			if f.count("SELECT COUNT(*) FROM company_applications WHERE id=?", created.CreatedApplicationID) != 0 || f.count("SELECT COUNT(*) FROM company_application_status_history WHERE application_id=?", created.CreatedApplicationID) != 0 || f.count("SELECT COUNT(*) FROM company_applications WHERE user_name='Nastya' AND vacancy_id=? AND company_name=?", id, crudMoveCompany) != 1 || f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name='Nastya' AND vacancy_id=? AND company_name=?", id, crudMoveCompany) != 1 {
				t.Fatal("concurrent owned deletion lost another user's moved links/history")
			}
			if _, e = repo.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrApplicationDeleted) {
				t.Fatal("concurrent delete/owner race revived dead intent")
			}
		})
	}
}

func TestMariaDBApplicationLinksHTTPDisposable(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, first := range []bool{true, false} {
			name := map[bool]string{true: "delete", false: "move"}[remove] + map[bool]string{true: " create first", false: " vacancy first"}[first]
			t.Run(name, func(t *testing.T) { runAppLinkMutation(t, remove, first, true) })
		}
	}
}
