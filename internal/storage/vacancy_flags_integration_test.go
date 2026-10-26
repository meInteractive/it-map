//go:build mariadb && vacancywrites

package storage

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/vacancies"
)

func TestMariaDBVacancyFlagsDisposable(t *testing.T) {
	t.Run("private flags preserve relevance revision and shared vacancy", func(t *testing.T) {
		f := openVacancyFlagFixture(t)
		f.seed("Demo", 63)
		f.seed("Nastya", 27)
		foreign := f.detail("Nastya")
		before := f.detail("Demo")
		for _, v := range []struct {
			key   string
			value bool
		}{{"favorite", true}, {"hidden", true}, {"favorite", false}, {"hidden", false}, {"favorite", true}, {"favorite", true}} {
			result := f.set("Demo", v.key, v.value)
			after := f.detail("Demo")
			expected := before
			expected.Vacancy.Favorite = result.Favorite
			expected.Vacancy.Hidden = result.Hidden
			if !reflect.DeepEqual(after, expected) || after.Vacancy.Relevance == nil || *after.Vacancy.Relevance != 63 {
				t.Fatal("vacancy flags changed unrelated detail/revision/relevance")
			}
			if v.key == "favorite" && (result.Favorite != v.value || result.Hidden != before.Vacancy.Hidden) || v.key == "hidden" && (result.Hidden != v.value || result.Favorite != before.Vacancy.Favorite) {
				t.Fatal("vacancy flags lost independent field")
			}
			before = after
		}
		if !reflect.DeepEqual(foreign, f.detail("Nastya")) {
			t.Fatal("vacancy flags changed another user")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Demo' AND vacancy_id=1`) != 1 {
			t.Fatal("flag retry duplicated private vacancy state")
		}
		list, err := f.repo.List(f.ctx, "Demo", nil, time.Now())
		if err != nil || len(list.Vacancies) != 1 || !list.Vacancies[0].Favorite || list.Vacancies[0].Hidden || list.Vacancies[0].Relevance == nil || *list.Vacancies[0].Relevance != 63 {
			t.Fatal("compact list readback lost own flag or relevance")
		}
		company, err := f.repo.CompanyVacancies(f.ctx, "Demo", "CI Synthetic Employer")
		if err != nil || len(company) != 1 || !company[0].Favorite || company[0].Hidden || company[0].Relevance == nil || *company[0].Relevance != 63 {
			t.Fatal("company readback lost own flags or relevance")
		}
	})
	t.Run("first write defaults unknown target UNIQUE and FK", func(t *testing.T) {
		f := openVacancyFlagFixture(t)
		got := f.set("Demo", "hidden", true)
		if got.VacancyID != 1 || !got.Hidden || got.Favorite || f.detail("Demo").Vacancy.Relevance != nil {
			t.Fatal("first flag write changed defaults")
		}
		if _, err := f.repo.SetUserFlag(f.ctx, "Demo", 999999, "favorite", true); !errors.Is(err, vacancies.ErrNotFound) {
			t.Fatal("unknown vacancy accepted")
		}
		if _, err := f.repo.SetUserFlag(f.ctx, "Demo", 1, "relevance", true); !errors.Is(err, vacancies.ErrInvalidFlag) {
			t.Fatal("unknown flag accepted")
		}
		for _, v := range []struct {
			id     int
			number uint16
		}{{1, 1062}, {999999, 1452}} {
			_, err := f.db.ExecContext(f.ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id) VALUES('Demo',?)`, v.id)
			var sqlErr *mysql.MySQLError
			if !errors.As(err, &sqlErr) || sqlErr.Number != v.number {
				t.Fatal("vacancy state UNIQUE/FK constraint not enforced")
			}
		}
	})
	t.Run("two independent SQL writers merge flags without duplicate state", func(t *testing.T) {
		f := openVacancyFlagFixture(t)
		ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
		defer cancel()
		var repos [2]vacancies.Repository
		var ids [2]int64
		for i := range repos {
			_, db := openDisposableMariaDB(t)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			if err := db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
				t.Fatal("cannot identify vacancy writer")
			}
			repos[i] = vacancies.Repository{DB: db}
		}
		if ids[0] == ids[1] {
			t.Fatal("vacancy flag concurrency requires independent connections")
		}
		holder, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("cannot start vacancy barrier")
		}
		var id int64
		if err = holder.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE id=1 FOR UPDATE`).Scan(&id); err != nil {
			holder.Rollback()
			t.Fatal("cannot hold vacancy row")
		}
		var wg sync.WaitGroup
		var outcomes [2]error
		defer func() { cancel(); holder.Rollback(); wg.Wait() }()
		for i, key := range []string{"favorite", "hidden"} {
			wg.Add(1)
			go func(i int, key string) {
				defer wg.Done()
				_, outcomes[i] = repos[i].SetUserFlag(ctx, "Demo", 1, key, true)
			}(i, key)
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			var waiting int
			if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE '%FROM company_vacancies WHERE id=%FOR UPDATE%'`, ids[0], ids[1]).Scan(&waiting); err != nil {
				t.Fatal("cannot observe vacancy row barrier")
			}
			if waiting == 2 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("vacancy writers never reached real row lock")
			case <-tick.C:
			}
		}
		if err = holder.Rollback(); err != nil {
			t.Fatal("cannot release vacancy barrier")
		}
		wg.Wait()
		for _, err := range outcomes {
			if err != nil {
				t.Fatalf("concurrent vacancy flag write failed: %T", err)
			}
		}
		got := f.detail("Demo")
		if !got.Vacancy.Favorite || !got.Vacancy.Hidden || f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Demo' AND vacancy_id=1`) != 1 {
			t.Fatal("concurrent vacancy flags lost update or duplicated state")
		}
	})
	t.Run("late real INSERT UPDATE faults rollback and fresh pool retry", func(t *testing.T) {
		for _, stage := range []string{"insert", "update"} {
			t.Run(stage, func(t *testing.T) {
				f := openVacancyFlagFixture(t)
				prefix := "INSERT INTO company_vacancy_user_state"
				if stage == "update" {
					f.seed("Demo", 63)
					prefix = "UPDATE company_vacancy_user_state SET"
				}
				before := f.detail("Demo")
				rows := f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Demo' AND vacancy_id=1`)
				fault, db := openMutationFaultPool(t, prefix)
				_, err := (vacancies.Repository{DB: db}).SetUserFlag(f.ctx, "Demo", 1, "favorite", true)
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("vacancy flag fault did not fire after real SQL before commit")
				}
				if err = db.Close(); err != nil {
					t.Fatal("cannot close interrupted vacancy pool")
				}
				if !reflect.DeepEqual(before, f.detail("Demo")) || rows != f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Demo' AND vacancy_id=1`) {
					t.Fatal("late vacancy flag fault left partial state")
				}
				_, fresh := openDisposableMariaDB(t)
				got, err := (vacancies.Repository{DB: fresh}).SetUserFlag(f.ctx, "Demo", 1, "favorite", true)
				if err != nil || !got.Favorite {
					t.Fatal("fresh pool vacancy flag retry failed")
				}
			})
		}
	})
}
