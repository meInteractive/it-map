//go:build mariadb && taskwrites

package storage

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"itmap/internal/tasks"
)

func TestMariaDBTaskWritesDisposable(t *testing.T) {
	t.Run("roundtrip dates filters and isolation", func(t *testing.T) {
		f := openTaskFixture(t)
		input := f.input()
		input.CompanyName = "\uFEFF" + input.CompanyName + "\uFEFF"
		input.Note = "\uFEFF" + input.Note + "\uFEFF"
		id, err := f.repo.Create(f.ctx, taskUser, input)
		if err != nil || id <= 1 {
			t.Fatalf("normalized task create failed: %T", err)
		}
		before := f.row(id)
		if before.CompanyName != taskCompany || before.Note != f.input().Note || before.DueAt != "2026-10-26T21:15:00.123Z" || before.VacancyID == nil || *before.VacancyID != 1 ||
			before.VacancyTitle != "CI Synthetic Vacancy" || before.Completed || before.CompletedAt != nil || before.CreatedAt == "" || before.GoogleEventID != "" {
			t.Fatal("task create lost normalized fields/join/defaults")
		}
		for _, user := range []string{"Eugene", "Nastya"} {
			if err := f.repo.SetCompleted(f.ctx, user, id, true); !errors.Is(err, tasks.ErrTaskNotFound) {
				t.Fatalf("foreign completion must be not found: %T", err)
			}
			if _, err := f.repo.Reschedule(f.ctx, user, id, "2026-10-28T12:00:00Z"); !errors.Is(err, tasks.ErrTaskNotFound) {
				t.Fatalf("foreign reschedule must be not found: %T", err)
			}
		}
		foreign, err := f.repo.List(f.ctx, "Nastya", "", true, nil)
		if err != nil || len(foreign.Tasks) != 0 {
			t.Fatal("personal tasks leaked to another user")
		}
		vacancy := int64(1)
		listed, err := f.repo.List(f.ctx, taskUser, taskCompany, false, &vacancy)
		if err != nil || len(listed.Tasks) != 1 || len(listed.Entities) != 1 || listed.Entities[0].Name != taskCompany {
			t.Fatal("task company/vacancy filters or entity directory failed")
		}
		missing := int64(999999)
		listed, err = f.repo.List(f.ctx, taskUser, taskCompany, false, &missing)
		if err != nil || len(listed.Tasks) != 0 {
			t.Fatal("vacancy filter ignored")
		}
		if _, err := f.repo.List(f.ctx, taskUser, "CI Unknown Synthetic Employer", false, nil); !errors.Is(err, tasks.ErrCompanyNotFound) {
			t.Fatalf("unknown task company accepted: %T", err)
		}
		due, err := f.repo.Reschedule(f.ctx, taskUser, id, "2026-10-28T23:15:00.987-03:30")
		if err != nil || due != "2026-10-29T02:45:00.987Z" {
			t.Fatalf("task deadline did not normalize across midnight: %T", err)
		}
		expected := before
		expected.DueAt = due
		if !reflect.DeepEqual(expected, f.row(id)) {
			t.Fatal("reschedule changed fields beyond due_at")
		}
		f.db.SetMaxOpenConns(1)
		f.db.SetMaxIdleConns(1)
		if _, err := f.db.ExecContext(f.ctx, "SET timestamp=1700000000"); err != nil {
			t.Fatal("cannot freeze task completion CI session")
		}
		if err := f.repo.SetCompleted(f.ctx, taskUser, id, true); err != nil {
			t.Fatalf("completion failed: %T", err)
		}
		completed := f.row(id)
		if !completed.Completed || completed.CompletedAt == nil || completed.DueAt != due {
			t.Fatal("completion lost timestamp/deadline")
		}
		if _, err := f.db.ExecContext(f.ctx, "SET timestamp=1700000060"); err != nil {
			t.Fatal("cannot advance synthetic CI session")
		}
		if err := f.repo.SetCompleted(f.ctx, taskUser, id, true); err != nil {
			t.Fatalf("idempotent completion failed: %T", err)
		}
		if !reflect.DeepEqual(completed, f.row(id)) {
			t.Fatal("repeated completion changed completed_at or task contents")
		}
		listed, err = f.repo.List(f.ctx, taskUser, "", false, nil)
		if err != nil || len(listed.Tasks) != 0 {
			t.Fatal("completed task remained in active list")
		}
		listed, err = f.repo.List(f.ctx, taskUser, "", true, nil)
		if err != nil || len(listed.Tasks) != 1 {
			t.Fatal("includeCompleted omitted completed task")
		}
		if err := f.repo.SetCompleted(f.ctx, taskUser, id, false); err != nil {
			t.Fatalf("reopening task failed: %T", err)
		}
		reopened := f.row(id)
		if reopened.Completed || reopened.CompletedAt != nil || reopened.DueAt != due {
			t.Fatal("reopen did not clear only completion fields")
		}
		for _, patch := range []struct {
			input tasks.CreateInput
			want  error
		}{
			{tasks.CreateInput{CompanyName: "CI Unknown Synthetic Employer", Type: "call", DueAt: input.DueAt}, tasks.ErrCompanyNotFound},
			{tasks.CreateInput{CompanyName: taskCompany, VacancyID: &missing, Type: "call", DueAt: input.DueAt}, tasks.ErrVacancyNotInCompany},
			{tasks.CreateInput{CompanyName: taskCompany, Type: "call", Note: strings.Repeat("😀", 601), DueAt: input.DueAt}, tasks.ErrInvalidTask},
		} {
			if _, err := f.repo.Create(f.ctx, taskUser, patch.input); !errors.Is(err, patch.want) {
				t.Fatalf("invalid task input accepted: %T", err)
			}
		}
		if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 1 {
			t.Fatal("rejected task operations left extra rows")
		}
	})
	t.Run("calendar guard preserves data", func(t *testing.T) {
		f := openTaskFixture(t)
		id := f.create()
		before := f.row(id)
		if _, err := f.db.ExecContext(f.ctx, "INSERT INTO google_calendar_user_connection(user_name,refresh_token_encrypted) VALUES(?,?)", taskUser, taskCalendarMarker); err != nil {
			t.Fatal("cannot seed synthetic calendar presence marker")
		}
		if _, err := f.repo.Create(f.ctx, taskUser, f.input()); !errors.Is(err, tasks.ErrCalendarSyncRequired) {
			t.Fatalf("connected Calendar task create must refuse: %T", err)
		}
		if _, err := f.repo.Reschedule(f.ctx, taskUser, id, "2026-11-01T12:00:00Z"); !errors.Is(err, tasks.ErrCalendarSyncRequired) {
			t.Fatalf("connected Calendar reschedule must refuse: %T", err)
		}
		if !reflect.DeepEqual(before, f.row(id)) {
			t.Fatal("Calendar guard changed task contents")
		}
		if _, err := f.db.ExecContext(f.ctx, "DELETE FROM google_calendar_user_connection WHERE user_name=? AND refresh_token_encrypted=?", taskUser, taskCalendarMarker); err != nil {
			t.Fatal("cannot remove synthetic calendar marker")
		}
		for _, status := range []string{"synced", "error", "pending"} {
			if _, err := f.db.ExecContext(f.ctx, "UPDATE company_tasks SET google_event_id=?,google_sync_status=? WHERE user_name=? AND id=?", "CI_SYNTHETIC_EVENT", status, taskUser, id); err != nil {
				t.Fatal("cannot seed synthetic event linkage")
			}
			linked := f.row(id)
			if _, err := f.repo.Reschedule(f.ctx, taskUser, id, "2026-11-02T12:00:00Z"); !errors.Is(err, tasks.ErrCalendarSyncRequired) {
				t.Fatalf("linked task reschedule must refuse: %T", err)
			}
			if !reflect.DeepEqual(linked, f.row(id)) {
				t.Fatal("linked Calendar rejection changed task")
			}
		}
		linked := f.row(id)
		if err := f.repo.SetCompleted(f.ctx, taskUser, id, true); err != nil {
			t.Fatalf("completion must preserve the legacy local-only contract: %T", err)
		}
		done := f.row(id)
		linked.Completed, linked.CompletedAt = done.Completed, done.CompletedAt
		if !done.Completed || done.CompletedAt == nil || !reflect.DeepEqual(linked, done) {
			t.Fatal("completion changed Calendar linkage or task fields")
		}
		if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 1 {
			t.Fatal("Calendar refusals left extra tasks")
		}
	})
	t.Run("real writes roll back on injected client error", testTaskWriteRollbacks)
}
