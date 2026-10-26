//go:build mariadb && crmwrites

package storage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"itmap/internal/crm"
)

func (f *crmFlagFixture) save(user, company, status, comment string) crm.SaveResult {
	f.t.Helper()
	got, err := f.repo.SaveCompany(f.ctx, user, company, crm.SaveInput{Status: status, Comment: comment})
	if err != nil {
		f.t.Fatalf("company CRM save failed: %T", err)
	}
	return got
}
func TestMariaDBCRMSaveDisposable(t *testing.T) {
	t.Run("whole form flags owner isolation history and vacancy response", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		f.seed("Nastya")
		foreign := f.state("Nastya", crmFlagCompany)
		f.set("Demo", crmFlagCompany, "favorite", true)
		f.set("Demo", crmFlagCompany, "hidden", true)
		got := f.save("Demo", crmFlagCompany, "Откликнулся", "  CI comment 😀 <&>  ")
		if got.Status != "Откликнулся" || got.Comment != "  CI comment 😀 <&>  " || !got.Favorite || !got.Hidden || got.UpdatedAt == nil || len(got.History) != 1 || got.History[0].FromStatus != "" || got.History[0].ToStatus != got.Status || got.History[0].ChangedAt == "" {
			t.Fatal("CRM save lost fields or initial history")
		}
		if len(got.Vacancies) != 1 || got.Vacancies[0].ID != 1 || got.Vacancies[0].ApplicationID != nil || got.Vacancies[0].ApplicationStatus != "" {
			t.Fatal("CRM save vacancy projection omitted shared vacancy or leaked another user's application")
		}
		got = f.save("Demo", crmFlagCompany, "Откликнулся", "CI comment-only edit")
		if len(got.History) != 1 {
			t.Fatal("same status appended duplicate history")
		}
		got = f.save("Demo", crmFlagCompany, "Откликнулся", "CI comment-only edit")
		if len(got.History) != 1 {
			t.Fatal("retry appended duplicate history")
		}
		got = f.save("Demo", crmFlagCompany, "", "")
		if got.Status != "" || got.Comment != "" || len(got.History) != 2 || got.History[0].FromStatus != "Откликнулся" || got.History[0].ToStatus != "" || !got.Favorite || !got.Hidden {
			t.Fatal("clear whole-form fields broke history or flags")
		}
		if !reflect.DeepEqual(foreign, f.state("Nastya", crmFlagCompany)) {
			t.Fatal("CRM save changed foreign state/history")
		}
		if f.count(`SELECT COUNT(*) FROM company_user_state WHERE user_name='Demo' AND company_name=?`, crmFlagCompany) != 1 {
			t.Fatal("CRM retry duplicated private row")
		}
	})
	t.Run("fresh company empty history and last thirty actual transitions", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		got := f.save("Demo", crmFreshCompany, "", "CI bootstrap")
		if got.History == nil || len(got.History) != 0 || got.Vacancies == nil || len(got.Vacancies) != 0 || got.Status != "" || got.Comment != "CI bootstrap" {
			t.Fatal("fresh company CRM response defaults changed")
		}
		previous := ""
		for i := 0; i < 36; i++ {
			status := "Откликнулся"
			if i%2 == 1 {
				status = "Получил ответ"
			}
			got = f.save("Demo", crmFreshCompany, status, "CI bounded history")
			if got.History[0].FromStatus != previous || got.History[0].ToStatus != status {
				t.Fatal("history chain/order differs from actual transitions")
			}
			previous = status
		}
		if len(got.History) != 30 || f.count(`SELECT COUNT(*) FROM company_status_history WHERE user_name='Demo' AND company_name=?`, crmFreshCompany) != 36 {
			t.Fatal("CRM read must limit history to last thirty without deleting durable history")
		}
	})
	t.Run("late SQL interruption rolls back state bootstrap and history", func(t *testing.T) {
		for _, stage := range []string{"bootstrap", "state", "history"} {
			t.Run(stage, func(t *testing.T) {
				f := openCRMFlagFixture(t)
				company := crmFreshCompany
				prefix := "INSERT INTO company_user_state"
				if stage != "bootstrap" {
					company = crmFlagCompany
					f.save("Demo", company, "Откликнулся", "CI before rollback")
					f.set("Demo", company, "favorite", true)
					prefix = "UPDATE company_user_state SET"
				}
				if stage == "history" {
					prefix = "INSERT INTO company_status_history"
				}
				before := f.state("Demo", company)
				count := f.count(`SELECT COUNT(*) FROM company_status_history WHERE user_name='Demo' AND company_name=?`, company)
				repo, fault, db := f.faultRepo(prefix)
				_, err := repo.SaveCompany(f.ctx, "Demo", company, crm.SaveInput{Status: "Получил ответ", Comment: "CI interrupted comment"})
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errCRMFlagFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("CRM save fault did not occur after real SQL before commit")
				}
				if err = db.Close(); err != nil {
					t.Fatal("cannot close interrupted CRM save pool")
				}
				if !reflect.DeepEqual(before, f.state("Demo", company)) || f.count(`SELECT COUNT(*) FROM company_status_history WHERE user_name='Demo' AND company_name=?`, company) != count {
					t.Fatal("interrupted CRM save left state or history")
				}
				if stage == "bootstrap" && f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, company) != 0 {
					t.Fatal("interrupted save left parent bootstrap")
				}
				_, fresh := openDisposableMariaDB(t)
				retry := (crm.Repository{DB: fresh})
				input := crm.SaveInput{Status: "Получил ответ", Comment: "CI successful retry"}
				if _, err = retry.SaveCompany(f.ctx, "Demo", company, input); err != nil {
					t.Fatalf("fresh pool retry failed: %T", err)
				}
				if _, err = retry.SaveCompany(f.ctx, "Demo", company, input); err != nil {
					t.Fatal("same-status retry failed")
				}
				if f.count(`SELECT COUNT(*) FROM company_status_history WHERE user_name='Demo' AND company_name=?`, company) != count+1 {
					t.Fatal("retry lost or duplicated CRM history")
				}
			})
		}
	})
	t.Run("simultaneous forms keep serial history and independent flag", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
		defer cancel()
		var repos [3]crm.Repository
		var ids [3]int64
		for i := range repos {
			_, pool := openDisposableMariaDB(t)
			pool.SetMaxOpenConns(1)
			pool.SetMaxIdleConns(1)
			if err := pool.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
				t.Fatal("cannot identify CRM save connection")
			}
			repos[i] = crm.Repository{DB: pool}
		}
		if ids[0] == ids[1] || ids[0] == ids[2] || ids[1] == ids[2] {
			t.Fatal("CRM concurrency requires distinct SQL connections")
		}
		holder, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("cannot begin CRM barrier")
		}
		var locked string
		if err = holder.QueryRowContext(ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, crmFlagCompany).Scan(&locked); err != nil {
			holder.Rollback()
			t.Fatal("cannot hold CRM barrier")
		}
		var wg sync.WaitGroup
		var outcomes [3]error
		defer func() { cancel(); holder.Rollback(); wg.Wait() }()
		inputs := []crm.SaveInput{{Status: "Откликнулся", Comment: "CI form A"}, {Status: "Получил ответ", Comment: "CI form B"}}
		for i := range repos {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if i < 2 {
					_, outcomes[i] = repos[i].SaveCompany(ctx, "Demo", crmFlagCompany, inputs[i])
				} else {
					_, outcomes[i] = repos[i].SetCompanyFlag(ctx, "Demo", crmFlagCompany, "favorite", true)
				}
			}(i)
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			var waiting int
			if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?,?) AND INFO LIKE 'INSERT%INTO company_crm%'`, ids[0], ids[1], ids[2]).Scan(&waiting); err != nil {
				t.Fatal("cannot observe CRM writers")
			}
			if waiting == 3 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("CRM writers did not reach SQL barrier")
			case <-tick.C:
			}
		}
		if err = holder.Rollback(); err != nil {
			t.Fatal("cannot release CRM barrier")
		}
		wg.Wait()
		for _, err := range outcomes {
			if err != nil {
				t.Fatalf("concurrent CRM save/flag failed: %T", err)
			}
		}
		got := f.state("Demo", crmFlagCompany)
		if !got.Favorite || len(got.History) != 2 || got.History[1].FromStatus != "" || got.History[0].FromStatus != got.History[1].ToStatus || got.History[0].ToStatus != got.Status || got.History[0].ToStatus == got.History[1].ToStatus {
			t.Fatal("concurrent forms lost serial history or favorite")
		}
		if got.Status == inputs[0].Status && got.Comment != inputs[0].Comment || got.Status == inputs[1].Status && got.Comment != inputs[1].Comment {
			t.Fatal("concurrent forms mixed status/comment from different saves")
		}
	})
}

func TestMariaDBCRMSaveHTTPDisposable(t *testing.T) {
	t.Run("signed save roundtrip validation isolation and full response", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		h := openCRMFlagHTTP(f, f.repo)
		path := "/api/crm?company=CI%20Synthetic%20Employer"
		h.error(h.call("", "PUT", path, crm.SaveInput{}, 401))
		for _, raw := range []string{`{"status":"CI unknown"}`, `{"status":"Откликнулся","status":"Офер"}`, `{"comment":true}`, `{"userName":"Nastya"}`, `{"revision":"invented"}`, `{} []`} {
			h.error(h.call("demo", "PUT", path, raw, 400))
		}
		h.error(h.call("demo", "PUT", "/api/crm?company=CI%20nonexistent%20company", crm.SaveInput{}, 404))
		if f.count(`SELECT COUNT(*) FROM company_user_state WHERE user_name='Demo' AND company_name=?`, crmFlagCompany) != 0 {
			t.Fatal("invalid CRM saves left private state")
		}
		input := crm.SaveInput{Status: "Откликнулся", Comment: "  CI HTTP <&> 😀  "}
		got := crmFlagJSON[crm.SaveResult](t, h.call("demo", "PUT", path+"&userName=Nastya", input, 200))
		if got.Status != input.Status || got.Comment != input.Comment || len(got.History) != 1 || len(got.Vacancies) != 1 || got.Vacancies[0].ApplicationID != nil {
			t.Fatal("CRM PUT response contract/user projection failed")
		}
		other := crmFlagJSON[crm.CompanyState](t, h.call("nastya", "GET", path, nil, 200))
		if other.Status != "" || other.Comment != "" || len(other.History) != 0 {
			t.Fatal("CRM HTTP save leaked to another owner")
		}
		got = crmFlagJSON[crm.SaveResult](t, h.call("demo", "PUT", path, input, 200))
		if len(got.History) != 1 {
			t.Fatal("HTTP retry duplicated history")
		}
		got = crmFlagJSON[crm.SaveResult](t, h.call("demo", "PUT", path, `{"status":null,"comment":null}`, 200))
		if got.Status != "" || got.Comment != "" || len(got.History) != 2 {
			t.Fatal("HTTP null fields failed to clear whole form")
		}
	})
	t.Run("HTTP late history fault generic error atomic rollback retry", func(t *testing.T) {
		f := openCRMFlagFixture(t)
		repo, fault, _ := f.faultRepo("INSERT INTO company_status_history")
		h := openCRMFlagHTTP(f, repo)
		path := "/api/crm?company=CI%20Synthetic%20Employer"
		input := crm.SaveInput{Status: "Откликнулся", Comment: "CI HTTP failure"}
		data := h.call("demo", "PUT", path, input, 503)
		h.error(data)
		if strings.Contains(string(data), errCRMFlagFault.Error()) {
			t.Fatal("CRM HTTP disclosed SQL details")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("CRM HTTP never reached real history insert")
		}
		before := f.state("Demo", crmFlagCompany)
		if before.Status != "" || before.Comment != "" || len(before.History) != 0 || before.UpdatedAt != nil {
			t.Fatal("HTTP failure left partial CRM state/history")
		}
		got := crmFlagJSON[crm.SaveResult](t, h.call("demo", "PUT", path, input, 200))
		if got.Status != input.Status || len(got.History) != 1 {
			t.Fatal("HTTP history retry failed")
		}
	})
}
