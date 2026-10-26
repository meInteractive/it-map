//go:build mariadb && crmwrites

package storage

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/crm"
)

func TestMariaDBCRMFlagsDisposable(t *testing.T) {
	t.Run("flags preserve status history owner and independent field", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		f.seed("Demo")
		f.seed("Nastya")
		foreign := f.state("Nastya", crmFlagCompany)
		before := f.state("Demo", crmFlagCompany)
		for _, v := range []struct {
			flag  string
			value bool
		}{{"favorite", true}, {"hidden", true}, {"favorite", false}, {"hidden", false}, {"favorite", true}, {"favorite", true}} {
			after := f.set("Demo", crmFlagCompany, v.flag, v.value)
			if after.Status != before.Status || after.Comment != before.Comment || !reflect.DeepEqual(after.History, before.History) || after.UpdatedAt == nil {
				t.Fatal("flag write lost CRM fields/history")
			}
			if v.flag == "favorite" && (after.Favorite != v.value || after.Hidden != before.Hidden) || v.flag == "hidden" && (after.Hidden != v.value || after.Favorite != before.Favorite) {
				t.Fatal("flag write lost independent field")
			}
			before = after
		}
		if !reflect.DeepEqual(foreign, f.state("Nastya", crmFlagCompany)) {
			t.Fatal("flag write changed another user")
		}
		if f.count(`SELECT COUNT(*) FROM company_user_state WHERE user_name='Demo' AND company_name=?`, crmFlagCompany) != 1 {
			t.Fatal("idempotent flag retry duplicated state")
		}
		names, e := f.repo.CompanyNames(f.ctx, "Demo", "favorite")
		if e != nil || !reflect.DeepEqual(names, []string{crmFlagCompany}) {
			t.Fatal("favorite list readback mismatch")
		}
		names, e = f.repo.CompanyNames(f.ctx, "Nastya", "favorite")
		if e != nil || len(names) != 0 {
			t.Fatal("favorite list leaked another owner")
		}
	})
	t.Run("bootstrap Unicode limits unknown company unique and foreign keys", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		got := f.set("Demo", crmFreshCompany, "hidden", true)
		if !got.Hidden || got.Favorite || got.Status != "" || got.Comment != "" || len(got.History) != 0 {
			t.Fatal("fresh CRM defaults changed")
		}
		if _, e := f.repo.SetCompanyFlag(f.ctx, "Demo", "CI nonexistent company", "favorite", true); !errors.Is(e, crm.ErrCompanyNotFound) {
			t.Fatal("unknown company accepted")
		}
		if _, e := f.repo.SetCompanyFlag(f.ctx, "Demo", crmFlagCompany, "status", true); !errors.Is(e, crm.ErrInvalidFlag) {
			t.Fatal("invalid flag accepted")
		}
		for _, c := range []struct {
			query  string
			args   []any
			number uint16
		}{
			{`INSERT INTO company_user_state(user_name,company_name) VALUES(?,?)`, []any{"Demo", crmFreshCompany}, 1062},
			{`INSERT INTO company_user_state(user_name,company_name) VALUES(?,?)`, []any{"Demo", "CI nonexistent company"}, 1452},
			{`INSERT INTO company_status_history(user_name,company_name) VALUES(?,?)`, []any{"Demo", "CI nonexistent company"}, 1452},
		} {
			_, e := f.db.ExecContext(f.ctx, c.query, c.args...)
			var sqlError *mysql.MySQLError
			if !errors.As(e, &sqlError) || sqlError.Number != c.number {
				t.Fatal("required CRM UNIQUE/FK constraint not enforced")
			}
		}
	})
	t.Run("concurrent independent flags serialize on two connections", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		var name string
		if e = tx.QueryRowContext(f.ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, crmFlagCompany).Scan(&name); e != nil {
			t.Fatal(e)
		}
		// Separate pools ensure genuinely independent connections, not a Go mutex.
		cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if e != nil {
			t.Fatal("bad guarded CRM configuration")
		}
		results := make(chan error, 2)
		for _, flag := range []string{"favorite", "hidden"} {
			connector, e := mysql.NewConnector(cfg)
			if e != nil {
				t.Fatal(e)
			}
			pool := openCRMIndependentPool(t, connector)
			go func(flag string) {
				_, e := (crm.Repository{DB: pool}).SetCompanyFlag(f.ctx, "Demo", crmFlagCompany, flag, true)
				results <- e
			}(flag)
		}
		deadline := time.NewTimer(4 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(15 * time.Millisecond)
		defer tick.Stop()
		for {
			var waiting int
			f.scan(`SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE DB=DATABASE() AND INFO LIKE 'INSERT%INTO company_crm%'`, nil, &waiting)
			if waiting == 2 {
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("both CRM writers did not reach the held SQL lock")
			case <-tick.C:
			}
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			if e = <-results; e != nil {
				t.Fatalf("concurrent CRM flag transaction failed: %T", e)
			}
		}
		got := f.state("Demo", crmFlagCompany)
		if !got.Favorite || !got.Hidden || f.count(`SELECT COUNT(*) FROM company_user_state WHERE user_name='Demo' AND company_name=?`, crmFlagCompany) != 1 {
			t.Fatal("concurrent flags lost update or created duplicates")
		}
	})
	t.Run("late real SQL failures rollback bootstrap and update then retry", func(t *testing.T) {
		for _, stage := range []string{"insert", "update"} {
			t.Run(stage, func(t *testing.T) {
				f := openCRMFlagFixture(t)
				company := crmFreshCompany
				prefix := "INSERT INTO company_user_state"
				if stage == "update" {
					company = crmFlagCompany
					f.seed("Demo")
					prefix = "UPDATE company_user_state SET"
				}
				before := f.state("Demo", company)
				repo, fault, db := f.faultRepo(prefix)
				_, e := repo.SetCompanyFlag(f.ctx, "Demo", company, "favorite", true)
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(e, errCRMFlagFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("fault did not fire after a real CRM SQL write before commit")
				}
				if e = db.Close(); e != nil {
					t.Fatal("cannot close interrupted CRM pool")
				}
				if !reflect.DeepEqual(before, f.state("Demo", company)) {
					t.Fatal("interrupted CRM write left fields/history")
				}
				if stage == "insert" && f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, company) != 0 {
					t.Fatal("failed bootstrap left global parent")
				}
				if !f.set("Demo", company, "favorite", true).Favorite {
					t.Fatal("CRM retry failed")
				}
			})
		}
	})
}

func openCRMIndependentPool(t *testing.T, c driver.Connector) *sql.DB {
	t.Helper()
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return db
}
