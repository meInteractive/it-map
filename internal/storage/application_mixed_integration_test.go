//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"itmap/internal/applications"
)

func (f *receiptFixture) seedChannelVacancy() (string, int64) {
	f.t.Helper()
	const owner = "CI Synthetic Shortcut Channel"
	const id = int64(900001)
	if f.count("SELECT COUNT(*) FROM company_crm WHERE company_name=?", owner) != 0 ||
		f.count("SELECT COUNT(*) FROM company_vacancies WHERE id=? OR company_name=?", id, owner) != 0 {
		f.t.Fatal("channel shortcut requires its own empty synthetic namespace")
	}
	// Register bounded cleanup before the first write, including partial setup.
	f.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []struct {
			query string
			args  []any
		}{
			{"DELETE h FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name=? AND a.company_name=?", []any{receiptUser, owner}},
			{"DELETE FROM company_applications WHERE user_name=? AND company_name=?", []any{receiptUser, owner}},
			{"DELETE FROM company_vacancies WHERE id=? AND company_name=?", []any{id, owner}},
			{"DELETE FROM company_crm WHERE company_name=?", []any{owner}},
		} {
			if _, err := f.db.ExecContext(ctx, q.query, q.args...); err != nil {
				f.t.Error("synthetic channel shortcut cleanup failed")
			}
		}
		var remaining int
		if err := f.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM company_crm WHERE company_name=?) +
   (SELECT COUNT(*) FROM company_vacancies WHERE id=? OR company_name=?)`, owner, id, owner).Scan(&remaining); err != nil || remaining != 0 {
			f.t.Error("channel shortcut leaked synthetic owner/vacancy rows")
		}
	})
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		f.t.Fatal("cannot begin synthetic channel setup")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(f.ctx, "INSERT INTO company_crm(company_name) VALUES(?)", owner); err != nil {
		f.t.Fatal("cannot seed synthetic channel owner")
	}
	if _, err := tx.ExecContext(f.ctx, `INSERT INTO company_vacancies(id,company_name,entity_type,url,title)
  VALUES(?,?,'channel','https://example.invalid/ci-shortcut-channel','CI Synthetic Channel Vacancy')`, id, owner); err != nil {
		f.t.Fatal("cannot seed synthetic channel vacancy")
	}
	if err := tx.Commit(); err != nil {
		f.t.Fatal("cannot commit synthetic channel setup")
	}
	return owner, id
}

func TestMariaDBApplicationMixedDisposable(t *testing.T) {
	t.Run("channel owner absent from catalog", func(t *testing.T) {
		f := openReceiptFixture(t)
		owner, id := f.seedChannelVacancy()
		h := openReceiptHTTP(f, f.repo)
		token := h.creationContext(h.demo)
		ordinary := f.input
		ordinary.CompanyName, ordinary.VacancyID, ordinary.CreationContext = owner, &id, token
		h.error(h.call(h.demo, "POST", "/api/applications/create", ordinary, 404), "")
		f.assertCounts(0, 0, 0)
		input := applications.VacancyApplyInput{CompanyName: owner, IdempotencyKey: receiptKey(1), CreationContext: token}
		path := fmt.Sprintf("/api/vacancies/%d/apply", id)
		created := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", path, input, 200))
		if !created.OK || created.Replayed || created.CreatedApplicationID <= 1 || created.Vacancy.CompanyName != owner || created.Vacancy.EntityType != "channel" ||
			created.Vacancy.ApplicationID == nil || *created.Vacancy.ApplicationID != created.CreatedApplicationID {
			t.Fatal("shortcut did not accept the actual channel owner outside the catalog")
		}
		replay := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", path+"/create", input, 200))
		if !replay.Replayed || replay.CreatedApplicationID != created.CreatedApplicationID {
			t.Fatal("channel shortcut lost durable replay")
		}
		f.assertCounts(1, 1, 1)
		if name, err := f.repo.Delete(f.ctx, receiptUser, created.CreatedApplicationID); err != nil || name != owner {
			t.Fatalf("cannot delete synthetic channel application: %T", err)
		}
		h.error(h.call(h.demo, "POST", path, input, 410), "application_deleted")
		f.assertCounts(0, 1, 0)
	})
	t.Run("update races delete", func(t *testing.T) {
		f := openReceiptFixture(t)
		before := f.createRecord()
		input := updateFromRecord(before)
		input.Status, input.Comment = "Скрининг", "CI synthetic update racing deletion"
		outcomes := f.concurrentWrites("SELECT id FROM company_applications WHERE id=? AND user_name=? FOR UPDATE", []any{before.ID, receiptUser},
			"%FROM company_applications%FOR UPDATE%", func(ctx context.Context, repo applications.Repository, i int) receiptOutcome {
				var name string
				var err error
				if i == 0 {
					name, err = repo.Update(ctx, receiptUser, before.ID, input)
				} else {
					name, err = repo.Delete(ctx, receiptUser, before.ID)
				}
				if err == nil && name != receiptCompany {
					return receiptOutcome{err: errors.New("mixed writer lost company identity")}
				}
				return receiptOutcome{err: err}
			})
		if outcomes[1].err != nil {
			t.Fatalf("delete must finish after a competing update: %T", outcomes[1].err)
		}
		if outcomes[0].err != nil && !errors.Is(outcomes[0].err, applications.ErrNotFound) {
			t.Fatalf("update must commit before delete or see a missing record: %T", outcomes[0].err)
		}
		f.assertCounts(0, 1, 0)
		if _, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, f.input); !errors.Is(err, applications.ErrApplicationDeleted) {
			t.Fatalf("mixed update/delete resurrected a deleted intent: %T", err)
		}
		f.assertCounts(0, 1, 0)
	})
	t.Run("replay races delete", func(t *testing.T) {
		f := openReceiptFixture(t)
		before := f.createRecord()
		// A single locking join holds BOTH canonical rows until both server-side
		// writers are waiting: replay on generation and delete on application.
		outcomes := f.concurrentWrites(`SELECT CONCAT(g.generation,':',a.id) FROM app_runtime_generation g
   JOIN company_applications a ON a.id=? WHERE g.id=1 AND a.user_name=? FOR UPDATE`, []any{before.ID, receiptUser},
			"%FOR UPDATE%", func(ctx context.Context, repo applications.Repository, i int) receiptOutcome {
				if i == 0 {
					result, err := repo.CreateWithReceipt(ctx, receiptUser, f.input)
					return receiptOutcome{result: result, err: err}
				}
				name, err := repo.Delete(ctx, receiptUser, before.ID)
				if err == nil && name != receiptCompany {
					return receiptOutcome{err: errors.New("delete lost company identity")}
				}
				return receiptOutcome{err: err}
			})
		if outcomes[1].err != nil {
			t.Fatalf("delete failed while replaying the same intent: %T", outcomes[1].err)
		}
		if outcomes[0].err == nil {
			if !outcomes[0].result.Replayed || outcomes[0].result.CreatedApplicationID != before.ID {
				t.Fatal("concurrent replay changed durable identity")
			}
		} else if !errors.Is(outcomes[0].err, applications.ErrApplicationDeleted) {
			t.Fatalf("unexpected concurrent replay/delete error: %T", outcomes[0].err)
		}
		f.assertCounts(0, 1, 0)
		if _, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, f.input); !errors.Is(err, applications.ErrApplicationDeleted) {
			t.Fatalf("replay/delete resurrected a deleted intent: %T", err)
		}
		f.assertCounts(0, 1, 0)
	})
}
