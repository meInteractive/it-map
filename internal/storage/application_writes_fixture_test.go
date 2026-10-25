//go:build mariadb && receiptwrites

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/applications"
)

const receiptUser = "Demo"
const receiptCompany = "CI Synthetic Employer"

type receiptFixture struct {
	t     *testing.T
	ctx   context.Context
	db    *sql.DB
	repo  applications.Repository
	input applications.CreateInput
}

func receiptKey(n int) string {
	return fmt.Sprintf("00000000-0000-4000-9000-%012d", n)
}

// Each test owns a preflight-empty synthetic user and a separate receipt key
// namespace. The existing GO-058 fixture and later readonly gates stay intact.
func openReceiptFixture(t *testing.T) *receiptFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("receipt writes require disposable GitHub CI")
		}
		t.Skip("receipt writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &receiptFixture{t: t, ctx: ctx, db: db,
		repo: applications.Repository{DB: db, CreationSecret: []byte(strings.Repeat("c", 64))}}
	if f.count("SELECT COUNT(*) FROM company_crm WHERE company_name=?", receiptCompany) != 1 ||
		f.count("SELECT COUNT(*) FROM company_applications WHERE user_name=?", receiptUser) != 0 ||
		f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?", receiptUser) != 0 {
		t.Fatal("receipt test requires the known empty Demo business fixture")
	}
	original, err := f.repo.ByID(ctx, "Eugene", 1)
	if err != nil {
		t.Fatal("original synthetic application unavailable")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []struct {
			query string
			args  []any
		}{
			{"DELETE FROM application_creation_receipts WHERE user_name=? AND idempotency_key LIKE ?", []any{receiptUser, "00000000-0000-4000-9000-%"}},
			{"DELETE h FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name=? AND a.company_name=?", []any{receiptUser, receiptCompany}},
			{"DELETE FROM company_applications WHERE user_name=? AND company_name=?", []any{receiptUser, receiptCompany}},
		} {
			if _, err := db.ExecContext(cleanup, q.query, q.args...); err != nil {
				t.Error("synthetic receipt cleanup failed")
			}
		}
		var remaining int
		if err := db.QueryRowContext(cleanup, `SELECT
			(SELECT COUNT(*) FROM company_applications WHERE user_name=?) +
			(SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?)`, receiptUser, receiptUser).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("receipt writes leaked into later readonly fixtures")
		}
		after, err := f.repo.ByID(cleanup, "Eugene", 1)
		if err != nil || !reflect.DeepEqual(original, after) {
			t.Error("another user's baseline application/history changed")
		}
		var generation string
		if err := db.QueryRowContext(cleanup, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("receipt writes changed the disposable generation")
		}
	})
	token, err := f.repo.CreationContext(ctx, receiptUser)
	if err != nil {
		t.Fatal("cannot issue synthetic creation context")
	}
	f.input = applications.CreateInput{CompanyName: receiptCompany, Status: "Откликнулся",
		Comment: "CI synthetic concurrent receipt", ApplicationSources: []string{"site"},
		IdempotencyKey: receiptKey(1), CreationContext: token}
	return f
}

func (f *receiptFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatal("cannot inspect synthetic receipt fixture")
	}
	return n
}

func (f *receiptFixture) assertCounts(apps, receipts, history int) {
	f.t.Helper()
	if f.count("SELECT COUNT(*) FROM company_applications WHERE user_name=?", receiptUser) != apps ||
		f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name=?", receiptUser) != receipts ||
		f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.user_name=?", receiptUser) != history {
		f.t.Fatal("unexpected durable application/receipt/history counts")
	}
}
