//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"itmap/internal/applications"
)

// This business test deliberately has an additional tag: the complete storage
// suite still runs on the EMPTY schema. CI runs this required write gate only
// after synthetic fixtures and readonly business tests, before HTTP/browser smoke.
func TestMariaDBApplicationReceiptsDisposable(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("receipt writes require disposable GitHub CI")
		}
		t.Skip("receipt writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	const user = "Demo"
	const company = "CI Synthetic Employer"
	key := func(number int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", number) }
	count := func(query string, args ...any) int {
		t.Helper()
		var value int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal("cannot inspect synthetic receipt fixture")
		}
		return value
	}
	if count("SELECT COUNT(*) FROM company_crm WHERE company_name=?", company) != 1 ||
		count("SELECT COUNT(*) FROM company_applications WHERE user_name=?", user) != 0 ||
		count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?", user) != 0 {
		t.Fatal("receipt test requires the known empty Demo business fixture")
	}
	// Committed writes are necessary to prove durable retry/deletion behavior.
	// Cleanup is bounded to a preflight-empty synthetic user/company/keyspace;
	// it never deletes Eugene's baseline rows or rotates the CI generation.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		queries := []struct {
			query string
			args  []any
		}{
			{"DELETE FROM application_creation_receipts WHERE user_name=? AND idempotency_key LIKE ?", []any{user, "00000000-0000-4000-8000-%"}},
			{"DELETE h FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name=? AND a.company_name=?", []any{user, company}},
			{"DELETE FROM company_applications WHERE user_name=? AND company_name=?", []any{user, company}},
		}
		for _, q := range queries {
			if _, err := db.ExecContext(cleanup, q.query, q.args...); err != nil {
				t.Error("synthetic receipt cleanup failed")
			}
		}
		var remaining int
		if err := db.QueryRowContext(cleanup, `SELECT
            (SELECT COUNT(*) FROM company_applications WHERE user_name=?) +
            (SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?)`, user, user).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("receipt writes leaked into later readonly API/browser fixtures")
		}
	})

	repo := applications.Repository{DB: db, CreationSecret: []byte(strings.Repeat("c", 64))}
	original, err := repo.ByID(ctx, "Eugene", 1)
	if err != nil {
		t.Fatal("original synthetic application unavailable")
	}
	token, err := repo.CreationContext(ctx, user)
	if err != nil {
		t.Fatal("cannot issue synthetic creation context")
	}
	vacancy := int64(1)
	input := applications.CreateInput{CompanyName: company, VacancyID: &vacancy, Status: "Откликнулся",
		Comment: "CI synthetic receipt", ApplicationSources: []string{"site"},
		IdempotencyKey: key(1), CreationContext: token}
	assertCounts := func(apps, receipts, history int) {
		t.Helper()
		if count("SELECT COUNT(*) FROM company_applications WHERE user_name=?", user) != apps ||
			count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?", user) != receipts ||
			count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name=?", user) != history {
			t.Fatal("unexpected durable application/receipt/history counts")
		}
	}
	created, err := repo.CreateWithReceipt(ctx, user, input)
	if err != nil || created.Replayed || created.CreatedApplicationID <= 1 || len(created.Records) != 1 || created.Limit != 10 {
		t.Fatalf("first durable creation failed: %T", err)
	}
	assertCounts(1, 1, 1)
	replayed, err := repo.CreateWithReceipt(ctx, user, input)
	if err != nil || !replayed.Replayed || replayed.CreatedApplicationID != created.CreatedApplicationID {
		t.Fatalf("retry lost durable identity: %T", err)
	}
	assertCounts(1, 1, 1)
	changed := input
	changed.Comment += " changed"
	if _, err := repo.CreateWithReceipt(ctx, user, changed); !errors.Is(err, applications.ErrIntentConflict) {
		t.Fatalf("changed intent must conflict: %T", err)
	}
	duplicate := input
	duplicate.IdempotencyKey = key(2)
	if _, err := repo.CreateWithReceipt(ctx, user, duplicate); !errors.Is(err, applications.ErrDuplicateVacancy) {
		t.Fatalf("another key must not duplicate a personal vacancy link: %T", err)
	}
	assertCounts(1, 1, 1)
	if _, err := repo.ByID(ctx, "Eugene", created.CreatedApplicationID); !errors.Is(err, applications.ErrNotFound) {
		t.Fatal("foreign application leaked to another synthetic user")
	}
	if _, err := repo.Delete(ctx, "Eugene", created.CreatedApplicationID); !errors.Is(err, applications.ErrNotFound) {
		t.Fatal("another user deleted the synthetic application")
	}
	assertCounts(1, 1, 1)

	var hash string
	if err := db.QueryRowContext(ctx, "SELECT request_hash FROM application_creation_receipts WHERE user_name=? AND idempotency_key=?", user, input.IdempotencyKey).Scan(&hash); err != nil {
		t.Fatal("durable receipt unavailable")
	}
	generation := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	if _, err := db.ExecContext(ctx, "INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash) VALUES(?,?,?,?)", user, key(98), generation, hash); err != nil {
		t.Fatal("cannot seed synthetic incomplete receipt")
	}
	incomplete := input
	incomplete.IdempotencyKey = key(98)
	if _, err := repo.CreateWithReceipt(ctx, user, incomplete); !errors.Is(err, applications.ErrIncompleteReceipt) {
		t.Fatalf("incomplete receipt must fail closed: %T", err)
	}
	staleGeneration := "0" + generation[1:]
	if staleGeneration == generation {
		staleGeneration = "1" + generation[1:]
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash,created_application_id) VALUES(?,?,?,?,?)", user, key(99), staleGeneration, hash, created.CreatedApplicationID); err != nil {
		t.Fatal("cannot seed synthetic old-generation receipt")
	}
	stale := input
	stale.IdempotencyKey = key(99)
	if _, err := repo.CreateWithReceipt(ctx, user, stale); !errors.Is(err, applications.ErrCreationContextExpired) {
		t.Fatalf("old-generation receipt must not be claimed: %T", err)
	}
	assertCounts(1, 3, 1)
	if name, err := repo.Delete(ctx, user, created.CreatedApplicationID); err != nil || name != company {
		t.Fatalf("synthetic delete failed: %T", err)
	}
	assertCounts(0, 3, 0)
	if _, err := repo.CreateWithReceipt(ctx, user, input); !errors.Is(err, applications.ErrApplicationDeleted) {
		t.Fatalf("deleted application was recreated by retry: %T", err)
	}
	assertCounts(0, 3, 0)
	missingID := int64(999999)
	invalid := input
	invalid.VacancyID, invalid.IdempotencyKey = &missingID, key(3)
	if _, err := repo.CreateWithReceipt(ctx, user, invalid); !errors.Is(err, applications.ErrInvalidVacancy) {
		t.Fatalf("missing vacancy must be rejected: %T", err)
	}
	assertCounts(0, 3, 0)
	for i := 0; i < 10; i++ {
		item := input
		item.VacancyID, item.IdempotencyKey = nil, key(20+i)
		result, err := repo.CreateWithReceipt(ctx, user, item)
		if err != nil || result.Replayed || len(result.Records) != i+1 {
			t.Fatalf("synthetic application below the per-user limit failed: %T", err)
		}
	}
	assertCounts(10, 13, 10)
	overflow := input
	overflow.VacancyID, overflow.IdempotencyKey = nil, key(30)
	if _, err := repo.CreateWithReceipt(ctx, user, overflow); !errors.Is(err, applications.ErrApplicationLimit) {
		t.Fatalf("eleventh application must be rejected: %T", err)
	}
	assertCounts(10, 13, 10)
	after, err := repo.ByID(ctx, "Eugene", 1)
	if err != nil || after.Revision != original.Revision || len(after.History) != len(original.History) {
		t.Fatal("other user's baseline application was changed")
	}
	var finalGeneration string
	if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&finalGeneration); err != nil || finalGeneration != generation {
		t.Fatal("receipt writes changed the disposable generation")
	}
}
