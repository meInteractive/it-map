//go:build mariadb && vacancycreates

package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

func TestMariaDBVacancyCreateDisposable(t *testing.T) {
	t.Run("complete company creation max-one pool projections and FK", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		f.db.SetMaxOpenConns(1)
		input := f.input("company")
		created := f.created(f.repo, input)
		f.assertLockFree()
		if created.ID <= 1 || created.CompanyName != crudCompany || created.EntityType != "company" || created.Title != input.Title || created.DescriptionMD != input.DescriptionMD || created.SalaryFrom == nil || *created.SalaryFrom != *input.SalaryFrom || created.SalaryTo == nil || *created.SalaryTo != *input.SalaryTo || created.Relevance == nil || *created.Relevance != 63 || created.Favorite || created.Hidden || created.CoverLetterSubject != input.CoverLetterSubject || created.CoverLetterMD != input.CoverLetterMD || created.CoverLetterScope != "shared" {
			t.Fatal("created company vacancy fields/projection changed")
		}
		if len(created.Roles) != 1 || created.Roles[0].ID != f.leaf || len(created.TelegramSources) != 1 || created.TelegramSources[0].ChannelID != f.channel || created.TelegramSources[0].MessageURL != "https://t.me/ci_go088_channel/123" {
			t.Fatal("create role deduplication or canonical source failed")
		}
		if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudCompany) != 1 || f.count(`SELECT COUNT(*) FROM vacancy_role_assignments WHERE vacancy_id=?`, created.ID) != 1 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?`, created.ID) != 1 || f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=? AND user_name='Demo'`, created.ID) != 1 {
			t.Fatal("creation left unexpected parent/role/source/private rows")
		}
		other, err := f.repo.DetailByID(f.ctx, "Nastya", created.ID)
		if err != nil || other.Vacancy.Relevance != nil || other.Vacancy.Favorite || other.Vacancy.Hidden {
			t.Fatal("create relevance leaked to another owner")
		}
		own, err := f.repo.DetailByID(f.ctx, "Demo", created.ID)
		if err != nil || len(own.Vacancy.Revision) != 64 || own.Vacancy.Relevance == nil || *own.Vacancy.Relevance != 63 {
			t.Fatal("new vacancy detail/revision/relevance unavailable")
		}
		if _, err = f.repo.CreateManual(f.ctx, "Demo", input); !errors.Is(err, vacancies.ErrDuplicateURL) {
			t.Fatal("duplicate URL was not rejected")
		}
		f.assertLockFree()
	})
	t.Run("channel pseudo-owner bootstrap and duplicate canonical message", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		input := f.input("channel")
		input.CompanyName = ""
		input.EntityType = "channel"
		created := f.created(f.repo, input)
		if created.CompanyName != fmt.Sprintf("__telegram_channel__:%d", f.channel) || created.EntityType != "channel" || len(created.TelegramSources) != 1 {
			t.Fatal("channel pseudo-owner or source was lost")
		}
		input.URL = crudURLPrefix + "same-message"
		input.TelegramMessageURL = "https://t.me/ci_go088_channel/123"
		if _, err := f.repo.CreateManual(f.ctx, "Demo", input); !errors.Is(err, vacancies.ErrDuplicateMessage) {
			t.Fatal("canonical message duplicate accepted")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancies WHERE url LIKE ?`, crudURLPrefix+"%") != 1 {
			t.Fatal("duplicate message left a vacancy")
		}
		f.assertLockFree()
	})
	t.Run("invalid owner roles and channel leave no rows", func(t *testing.T) {
		for _, stage := range []string{"owner", "parent role", "unknown role", "unknown channel", "wrong message channel"} {
			t.Run(stage, func(t *testing.T) {
				f := openVacancyCreateFixture(t)
				input := f.input("invalid")
				want := vacancies.ErrInvalidOwner
				switch stage {
				case "owner":
					input.CompanyName = "CI unknown owner"
				case "parent role":
					input.RoleIDs = []int64{f.root}
					want = vacancies.ErrInvalidRole
				case "unknown role":
					input.RoleIDs = []int64{999999}
					want = vacancies.ErrInvalidRole
				case "unknown channel":
					id := int64(999999)
					input.TelegramChannelID = &id
					want = vacancies.ErrInvalidChannel
				case "wrong message channel":
					input.TelegramMessageURL = "https://t.me/ci_other_channel/123"
					want = vacancies.ErrInvalidChannel
				}
				if _, err := f.repo.CreateManual(f.ctx, "Demo", input); !errors.Is(err, want) {
					t.Fatalf("invalid create did not return expected error: %T", err)
				}
				f.assertAbsent()
				f.assertLockFree()
			})
		}
	})
	t.Run("real late SQL faults rollback all stages and retry new pool", func(t *testing.T) {
		for _, prefix := range []string{"INSERT INTO company_crm", "INSERT INTO company_vacancies", "INSERT INTO vacancy_role_assignments", "INSERT INTO vacancy_telegram_sources", "INSERT INTO company_vacancy_user_state"} {
			t.Run(prefix, func(t *testing.T) {
				f := openVacancyCreateFixture(t)
				fault, db := openMutationFaultPool(t, prefix)
				db.SetMaxOpenConns(1)
				input := f.input("rollback")
				_, err := (vacancies.Repository{DB: db}).CreateManual(f.ctx, "Demo", input)
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("fault did not fire after a real creation SQL write before commit")
				}
				f.assertAbsent()
				f.assertLockFree()
				if err = db.Close(); err != nil {
					t.Fatal("cannot close interrupted creator pool")
				}
				_, fresh := openDisposableMariaDB(t)
				fresh.SetMaxOpenConns(1)
				created := f.created(vacancies.Repository{DB: fresh}, input)
				if created.ID <= 1 || f.count(`SELECT COUNT(*) FROM company_vacancies WHERE url=?`, input.URL) != 1 {
					t.Fatal("fresh-pool retry lost or duplicated vacancy")
				}
				f.assertLockFree()
			})
		}
	})
	t.Run("simultaneous URL and message duplicates create exactly once", func(t *testing.T) {
		for _, kind := range []string{"URL", "message"} {
			t.Run(kind, func(t *testing.T) { testConcurrentVacancyCreate(t, kind) })
		}
	})
}
func testConcurrentVacancyCreate(t *testing.T, kind string) {
	f := openVacancyCreateFixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	var repos [2]vacancies.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err := db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
			t.Fatal("cannot identify vacancy creator")
		}
		repos[i] = vacancies.Repository{DB: db}
	}
	if ids[0] == ids[1] {
		t.Fatal("create race requires two SQL connections")
	}
	holder, err := f.db.Conn(ctx)
	if err != nil {
		t.Fatal("cannot hold named-lock barrier")
	}
	defer holder.Close()
	var acquired int
	if err = holder.QueryRowContext(ctx, `SELECT GET_LOCK('itmap:manual-vacancy:create',0)`).Scan(&acquired); err != nil || acquired != 1 {
		t.Fatal("cannot acquire creation barrier")
	}
	release := func() {
		cleanup, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		holder.ExecContext(cleanup, `DO RELEASE_LOCK('itmap:manual-vacancy:create')`)
	}
	var wg sync.WaitGroup
	var outcomes [2]error
	defer func() { cancel(); release(); wg.Wait() }()
	first := f.input("concurrent")
	second := first
	if kind == "URL" {
		second.TelegramMessageURL = "https://t.me/ci_go088_channel/124"
	} else {
		second.URL = crudURLPrefix + "concurrent-message"
	}
	for i, input := range []vacancies.Input{first, second} {
		wg.Add(1)
		go func(i int, input vacancies.Input) {
			defer wg.Done()
			_, outcomes[i] = repos[i].CreateManual(ctx, "Demo", input)
		}(i, input)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE '%GET_LOCK(%manual-vacancy:create%'`, ids[0], ids[1]).Scan(&waiting); err != nil {
			t.Fatal("cannot observe both named-lock waiters")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both creator connections never reached SQL barrier")
		case <-tick.C:
		}
	}
	release()
	wg.Wait()
	success, duplicate := 0, 0
	want := vacancies.ErrDuplicateURL
	if kind == "message" {
		want = vacancies.ErrDuplicateMessage
	}
	for _, err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, want) {
			duplicate++
		} else {
			t.Fatalf("unexpected concurrent creation error: %T", err)
		}
	}
	if success != 1 || duplicate != 1 || f.count(`SELECT COUNT(*) FROM company_vacancies WHERE url LIKE ?`, crudURLPrefix+"%") != 1 || f.count(`SELECT COUNT(*) FROM vacancy_role_assignments WHERE role_id=?`, f.leaf) != 1 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE channel_id=?`, f.channel) != 1 {
		t.Fatal("concurrent creation duplicated vacancy or related rows")
	}
	f.assertLockFree()
	// No ordering assumption: either request may acquire GET_LOCK first.
	own, err := f.repo.CompanyVacancies(f.ctx, "Demo", crudCompany)
	if err != nil || len(own) != 1 || own[0].Relevance == nil || *own[0].Relevance != 63 {
		t.Fatal("concurrent winner readback failed")
	}
	foreign, err := f.repo.CompanyVacancies(f.ctx, "Nastya", crudCompany)
	if err != nil || len(foreign) != 1 || foreign[0].Relevance != nil {
		t.Fatal("concurrent winner private state leaked")
	}
	clone := own[0]
	clone.Relevance = nil
	if !reflect.DeepEqual(clone, foreign[0]) {
		t.Fatal("foreign projection changed shared winning vacancy")
	}
}
