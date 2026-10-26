//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"itmap/internal/applications"
)

func TestMariaDBApplicationShortcutDisposable(t *testing.T) {
	t.Run("HTTP aliases fixed fields and sticky receipts", func(t *testing.T) {
		f := openReceiptFixture(t)
		h := openReceiptHTTP(f, f.repo)
		input := applications.VacancyApplyInput{CompanyName: receiptCompany, IdempotencyKey: receiptKey(1), CreationContext: h.creationContext(h.demo)}
		path := "/api/vacancies/1/apply"
		alias := path + "/create"
		h.error(h.call(h.anonymous, "POST", path, input, 401), "")
		h.error(h.call(h.nastya, "POST", path, input, 409), "creation_context_expired")
		h.error(h.call(h.demo, "POST", path, input, 403, http.Header{"Origin": {"https://cross-site.invalid"}}), "")
		h.error(h.call(h.demo, "POST", "/api/vacancies/0/apply", input, 400), "")
		h.error(h.call(h.demo, "POST", "/api/vacancies/999999/apply", input, 404), "")
		missing := input
		missing.CreationContext = ""
		h.error(h.call(h.demo, "POST", path, missing, 428), "creation_context_required")
		owner := input
		owner.CompanyName = "CI Synthetic Wrong Owner"
		h.error(h.call(h.demo, "POST", path, owner, 409), "vacancy_owner_changed")
		f.assertCounts(0, 0, 0)
		// These fields are deliberately ignored: shortcut intent is determined by
		// the route vacancy and its fixed status/sources, never the client hints.
		raw := map[string]any{"companyName": input.CompanyName, "idempotencyKey": input.IdempotencyKey, "creationContext": input.CreationContext,
			"vacancyId": 999999, "userName": "Eugene", "status": "Отказ", "comment": "CI ignored shortcut comment",
			"recruiterName": "CI Ignored Contact", "applicationSources": []string{"site"}}
		created := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", path, raw, 200))
		if !created.OK || created.Replayed || created.CreatedApplicationID <= 1 || created.Vacancy.ID != 1 ||
			created.Vacancy.ApplicationID == nil || *created.Vacancy.ApplicationID != created.CreatedApplicationID {
			t.Fatal("shortcut lost authoritative vacancy/created identity")
		}
		row, err := f.repo.ByID(f.ctx, receiptUser, created.CreatedApplicationID)
		if err != nil || row.VacancyID == nil || *row.VacancyID != 1 || row.Status != "Откликнулся" || row.Comment != "" ||
			row.RecruiterName != "" || row.RecruiterPhone != "" || row.RecruiterEmail != "" || row.RecruiterContactType != "" ||
			!reflect.DeepEqual(row.ApplicationSources, []string{"hh"}) || len(row.History) != 1 {
			t.Fatal("shortcut accepted client-supplied fields or lost initial history")
		}
		replay := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", alias, input, 200))
		if !replay.Replayed || replay.CreatedApplicationID != row.ID {
			t.Fatal("shortcut alias did not replay the same identity")
		}
		second := input
		second.IdempotencyKey = receiptKey(2)
		claimed := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", alias, second, 200))
		if claimed.Replayed || claimed.CreatedApplicationID != row.ID {
			t.Fatal("new shortcut intent did not claim the existing application")
		}
		after, err := f.repo.ByID(f.ctx, receiptUser, row.ID)
		if err != nil || !reflect.DeepEqual(row, after) {
			t.Fatal("claiming existing application changed fields/revision/history")
		}
		f.assertCounts(1, 2, 1)
		h.call(h.demo, "DELETE", fmt.Sprintf("/api/applications/%d", row.ID), nil, 200)
		h.error(h.call(h.demo, "POST", path, input, 410), "application_deleted")
		h.error(h.call(h.demo, "POST", alias, second, 410), "application_deleted")
		f.assertCounts(0, 2, 0)
	})
	t.Run("claim edited application at ten limit", func(t *testing.T) {
		f := openReceiptFixture(t)
		vacancy := int64(1)
		input := f.input
		input.VacancyID = &vacancy
		created, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, input)
		if err != nil {
			t.Fatalf("cannot prepare linked manual application: %T", err)
		}
		row, err := f.repo.ByID(f.ctx, receiptUser, created.CreatedApplicationID)
		if err != nil {
			t.Fatal("cannot read linked manual application")
		}
		edit := updateFromRecord(row)
		edit.Status, edit.Comment = "Скрининг", "CI synthetic edited manual application"
		edit.RecruiterName, edit.RecruiterEmail, edit.RecruiterContactType = "CI Synthetic Recruiter", "ci-recruiter@example.invalid", "direct"
		if _, err := f.repo.Update(f.ctx, receiptUser, row.ID, edit); err != nil {
			t.Fatalf("cannot prepare edited application: %T", err)
		}
		before, err := f.repo.ByID(f.ctx, receiptUser, row.ID)
		if err != nil {
			t.Fatal("cannot read edited application")
		}
		for i := 0; i < 9; i++ {
			seed := f.input
			seed.IdempotencyKey = receiptKey(20 + i)
			if _, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, seed); err != nil {
				t.Fatalf("cannot prepare synthetic application limit: %T", err)
			}
		}
		f.assertCounts(10, 10, 11)
		h := openReceiptHTTP(f, f.repo)
		shortcut := applications.VacancyApplyInput{CompanyName: receiptCompany, IdempotencyKey: receiptKey(99), CreationContext: h.creationContext(h.demo)}
		claimed := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", "/api/vacancies/1/apply", shortcut, 200))
		if !claimed.OK || claimed.Replayed || claimed.CreatedApplicationID != before.ID {
			t.Fatal("ten limit incorrectly rejected claiming an existing application")
		}
		replay := receiptHTTPJSON[applications.VacancyApplyResult](t, h.call(h.demo, "POST", "/api/vacancies/1/apply/create", shortcut, 200))
		if !replay.Replayed || replay.CreatedApplicationID != before.ID {
			t.Fatal("claim at ten limit lost durable replay")
		}
		after, err := f.repo.ByID(f.ctx, receiptUser, before.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("shortcut at ten limit overwrote edited status/contacts/comment/sources/history")
		}
		overflow := f.input
		overflow.IdempotencyKey = receiptKey(100)
		h.error(h.call(h.demo, "POST", "/api/applications/create", overflow, 400), "")
		f.assertCounts(10, 11, 11)
	})
	t.Run("ordinary create races shortcut", func(t *testing.T) {
		f := openReceiptFixture(t)
		vacancy := int64(1)
		ordinary := f.input
		ordinary.VacancyID = &vacancy
		shortcut := applications.VacancyApplyInput{CompanyName: receiptCompany, IdempotencyKey: receiptKey(2), CreationContext: f.input.CreationContext}
		outcomes := f.concurrentWrites("SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE", nil,
			"SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE",
			func(ctx context.Context, repo applications.Repository, i int) receiptOutcome {
				if i == 0 {
					result, err := repo.CreateWithReceipt(ctx, receiptUser, ordinary)
					return receiptOutcome{result: result, err: err}
				}
				result, err := repo.ApplyToVacancy(ctx, receiptUser, 1, shortcut)
				return receiptOutcome{result: applications.CreationResult{CreatedApplicationID: result.CreatedApplicationID, Replayed: result.Replayed}, err: err}
			})
		if outcomes[1].err != nil || outcomes[1].result.Replayed || outcomes[1].result.CreatedApplicationID <= 1 {
			t.Fatalf("shortcut must create or claim a durable winner: %T", outcomes[1].err)
		}
		receipts := 1
		expectedComment := ""
		expectedSources := []string{"hh"}
		if outcomes[0].err == nil {
			receipts = 2
			expectedComment = ordinary.Comment
			expectedSources = ordinary.ApplicationSources
			if outcomes[0].result.Replayed || outcomes[0].result.CreatedApplicationID != outcomes[1].result.CreatedApplicationID {
				t.Fatal("ordinary/shortcut writers returned different identities")
			}
		} else if !errors.Is(outcomes[0].err, applications.ErrDuplicateVacancy) {
			t.Fatalf("unexpected ordinary writer error: %T", outcomes[0].err)
		}
		row, err := f.repo.ByID(f.ctx, receiptUser, outcomes[1].result.CreatedApplicationID)
		if err != nil || row.Comment != expectedComment || !reflect.DeepEqual(row.ApplicationSources, expectedSources) || len(row.History) != 1 {
			t.Fatal("racing shortcut overwrote the winner or duplicated history")
		}
		replay, err := f.repo.ApplyToVacancy(f.ctx, receiptUser, 1, shortcut)
		if err != nil || !replay.Replayed || replay.CreatedApplicationID != row.ID {
			t.Fatalf("racing shortcut cannot be replayed: %T", err)
		}
		retried, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, ordinary)
		if outcomes[0].err == nil {
			if err != nil || !retried.Replayed || retried.CreatedApplicationID != row.ID {
				t.Fatalf("ordinary winner cannot replay: %T", err)
			}
		} else if !errors.Is(err, applications.ErrDuplicateVacancy) {
			t.Fatalf("losing ordinary retry must remain a duplicate: %T", err)
		}
		f.assertCounts(1, receipts, 1)
	})
}
