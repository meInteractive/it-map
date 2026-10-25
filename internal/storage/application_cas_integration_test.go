//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/applications"
)

func updateFromRecord(row applications.Record) applications.UpdateInput {
	return applications.UpdateInput{Revision: row.Revision, VacancyID: row.VacancyID, Status: row.Status, Comment: row.Comment,
		RecruiterName: row.RecruiterName, RecruiterPhone: row.RecruiterPhone, RecruiterEmail: row.RecruiterEmail,
		RecruiterContactType: row.RecruiterContactType, ApplicationSources: row.ApplicationSources}
}
func (f *receiptFixture) createRecord() applications.Record {
	f.t.Helper()
	created, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, f.input)
	if err != nil {
		f.t.Fatalf("cannot create synthetic CAS fixture: %T", err)
	}
	row, err := f.repo.ByID(f.ctx, receiptUser, created.CreatedApplicationID)
	if err != nil {
		f.t.Fatal("cannot read synthetic CAS fixture")
	}
	return row
}

func TestMariaDBApplicationCASDisposable(t *testing.T) {
	t.Run("same second and unchanged update", func(t *testing.T) {
		f := openReceiptFixture(t)
		// Freeze only this physical CI session, never the server/global clock. Real
		// UTC_TIMESTAMP()/defaults then exercise identical-second CAS/clientFoundRows.
		f.db.SetMaxOpenConns(1)
		f.db.SetMaxIdleConns(1)
		if _, err := f.db.ExecContext(f.ctx, "SET timestamp=1700000000"); err != nil {
			t.Fatal("cannot freeze synthetic CAS session time")
		}
		before := f.createRecord()
		input := updateFromRecord(before)
		input.Status, input.Comment = "Скрининг", "CI synthetic same-second update"
		if name, err := f.repo.Update(f.ctx, receiptUser, before.ID, input); err != nil || name != receiptCompany {
			t.Fatalf("same-second update failed: %T", err)
		}
		after, err := f.repo.ByID(f.ctx, receiptUser, before.ID)
		if err != nil || after.Revision == before.Revision || after.UpdatedAt != before.UpdatedAt || after.Comment != input.Comment || len(after.History) != 2 {
			t.Fatal("revision must detect content changes within the same timestamp")
		}
		if _, err := f.repo.Update(f.ctx, receiptUser, before.ID, input); !errors.Is(err, applications.ErrConflict) {
			t.Fatalf("same-second stale revision must conflict: %T", err)
		}
		unchanged := updateFromRecord(after)
		if _, err := f.repo.Update(f.ctx, receiptUser, after.ID, unchanged); err != nil {
			t.Fatalf("unchanged UPDATE needs matched row semantics, not changed rows: %T", err)
		}
		same, err := f.repo.ByID(f.ctx, receiptUser, after.ID)
		if err != nil || !reflect.DeepEqual(after, same) {
			t.Fatal("unchanged update changed snapshot/history")
		}
		unchanged.Comment = "CI synthetic comment-only same-second update"
		if _, err := f.repo.Update(f.ctx, receiptUser, after.ID, unchanged); err != nil {
			t.Fatalf("comment-only update failed: %T", err)
		}
		final, err := f.repo.ByID(f.ctx, receiptUser, after.ID)
		if err != nil || final.Revision == after.Revision || final.UpdatedAt != after.UpdatedAt || len(final.History) != 2 || final.Comment != unchanged.Comment {
			t.Fatal("comment-only CAS must change revision without appending status history")
		}
		if _, err := f.repo.Update(f.ctx, "Nastya", after.ID, updateFromRecord(final)); !errors.Is(err, applications.ErrNotFound) {
			t.Fatalf("another user updated a synthetic application: %T", err)
		}
		f.assertCounts(1, 1, 2)
	})
	t.Run("two writers one revision", func(t *testing.T) {
		f := openReceiptFixture(t)
		before := f.createRecord()
		inputs := [2]applications.UpdateInput{updateFromRecord(before), updateFromRecord(before)}
		inputs[0].Status, inputs[0].Comment = "Скрининг", "CI synthetic CAS writer zero"
		inputs[1].Status, inputs[1].Comment = "Отказ", "CI synthetic CAS writer one"
		outcomes := f.concurrentWrites("SELECT id FROM company_applications WHERE id=? AND user_name=? FOR UPDATE",
			[]any{before.ID, receiptUser}, "%FROM company_applications a WHERE a.id=%FOR UPDATE%",
			func(ctx context.Context, repo applications.Repository, i int) receiptOutcome {
				name, err := repo.Update(ctx, receiptUser, before.ID, inputs[i])
				if err == nil && name != receiptCompany {
					return receiptOutcome{err: errors.New("CAS lost company identity")}
				}
				return receiptOutcome{err: err}
			})
		winner, conflicts := -1, 0
		for i, out := range outcomes {
			if out.err == nil {
				if winner != -1 {
					t.Fatal("both stale-revision writers committed")
				}
				winner = i
			} else if errors.Is(out.err, applications.ErrConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected concurrent CAS error: %T", out.err)
			}
		}
		if winner < 0 || conflicts != 1 {
			t.Fatal("CAS needs exactly one winner and one conflict")
		}
		after, err := f.repo.ByID(f.ctx, receiptUser, before.ID)
		if err != nil || after.Revision == before.Revision || after.Comment != inputs[winner].Comment || after.Status != inputs[winner].Status || len(after.History) != 2 {
			t.Fatal("concurrent CAS lost winner fields or appended false history")
		}
		if after.History[0].FromStatus != before.Status || after.History[0].ToStatus != after.Status {
			t.Fatal("CAS history has incorrect transition")
		}
		for _, input := range inputs {
			if _, err := f.repo.Update(f.ctx, receiptUser, before.ID, input); !errors.Is(err, applications.ErrConflict) {
				t.Fatalf("stale retry must conflict: %T", err)
			}
		}
		final, err := f.repo.ByID(f.ctx, receiptUser, before.ID)
		if err != nil || !reflect.DeepEqual(after, final) {
			t.Fatal("stale retry overwrote winner or history")
		}
		f.assertCounts(1, 1, 2)
	})
	t.Run("history failure rolls back update", func(t *testing.T) {
		f := openReceiptFixture(t)
		before := f.createRecord()
		cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if err != nil {
			t.Fatal("cannot configure guarded CAS fault connection")
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatal("cannot initialize guarded CAS fault connector")
		}
		fault := &receiptFault{stage: "history"}
		db := sql.OpenDB(receiptFaultConnector{Connector: connector, fault: fault})
		t.Cleanup(func() { db.Close() })
		repo := applications.Repository{DB: db}
		input := updateFromRecord(before)
		input.Status, input.Comment = "Скрининг", "CI synthetic rolled-back CAS update"
		_, err = repo.Update(f.ctx, receiptUser, before.ID, input)
		if !errors.Is(err, errReceiptInterruption) || !fault.fired || fault.updates != 1 || fault.committed || fault.history != 0 {
			t.Fatalf("expected armed history error after real UPDATE: %T", err)
		}
		if err := db.Close(); err != nil {
			t.Fatal("cannot close interrupted CAS pool")
		}
		after, err := f.repo.ByID(f.ctx, receiptUser, before.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("history failure did not roll back fields/revision/history")
		}
		f.assertCounts(1, 1, 1)
		if _, err := f.repo.Update(f.ctx, receiptUser, before.ID, input); err != nil {
			t.Fatalf("original revision must remain usable after rollback: %T", err)
		}
		f.assertCounts(1, 1, 2)
	})
}
