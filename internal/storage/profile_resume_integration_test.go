//go:build mariadb && profilewrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/profiles"
)

func TestMariaDBProfileResumeDisposable(t *testing.T) {
	t.Run("read-only defaults never initialize profile rows", func(t *testing.T) {
		f := openProfileResumeFixture(t)
		fault, db := openMutationFaultPool(t, "")
		got, err := (profiles.Repository{DB: db}).ReadResume(f.ctx, "Demo")
		fault.mu.Lock()
		writes := fault.writes
		fault.mu.Unlock()
		if err != nil || !reflect.DeepEqual(got, profiles.DefaultResume()) || writes != 0 || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`) != 0 {
			t.Fatal("profile GET changed storage or defaults")
		}
	})
	t.Run("Unicode points CAS clear owner isolation and schema constraints", func(t *testing.T) {
		f := openProfileResumeFixture(t)
		text := strings.Repeat("😀", profiles.MaxResumeLength)
		created := f.save("Demo", text, 0)
		if created.Revision != 1 || created.ResumeMD != text || created.UpdatedAt == nil || created.MaxLength != 20000 || f.count(`SELECT CHAR_LENGTH(resume_md) FROM user_profiles WHERE user_name='Demo'`) != 20000 || f.count(`SELECT OCTET_LENGTH(resume_md) FROM user_profiles WHERE user_name='Demo'`) != 80000 {
			t.Fatal("profile save lost Unicode text, version or limit projection")
		}
		if !reflect.DeepEqual(f.read("Nastya"), profiles.DefaultResume()) {
			t.Fatal("personal profile leaked to another owner")
		}
		if _, err := f.repo.SaveResume(f.ctx, "Demo", profiles.TextInput{Text: "CI stale", Revision: 0}); !errors.Is(err, profiles.ErrConflict) {
			t.Fatal("stale profile revision accepted")
		}
		if _, err := f.repo.SaveResume(f.ctx, "Nastya", profiles.TextInput{Text: "CI stale missing", Revision: 1}); !errors.Is(err, profiles.ErrConflict) || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Nastya'`) != 0 {
			t.Fatal("stale absent profile left a bootstrap row")
		}
		if !reflect.DeepEqual(created, f.read("Demo")) {
			t.Fatal("conflicting profile write changed committed data")
		}
		cleared := f.save("Demo", "", 1)
		if cleared.Revision != 2 || cleared.ResumeMD != "" || cleared.UpdatedAt == nil {
			t.Fatal("empty resume did not clear with version increment")
		}
		for _, v := range []struct {
			sql    string
			args   []any
			number uint16
		}{
			{`INSERT INTO user_profiles(user_name) VALUES('Demo')`, nil, 1062},
			{`UPDATE user_profiles SET revision=-1 WHERE user_name='Demo'`, nil, 4025},
			{`UPDATE user_profiles SET resume_md=? WHERE user_name='Demo'`, []any{strings.Repeat("x", 20001)}, 4025},
		} {
			_, err := f.db.ExecContext(f.ctx, v.sql, v.args...)
			var sqlErr *mysql.MySQLError
			if !errors.As(err, &sqlErr) || sqlErr.Number != v.number {
				t.Fatal("profile primary key/revision/text CHECK was not enforced")
			}
		}
		rows, err := f.db.QueryContext(f.ctx, `SELECT COLUMN_NAME,DATA_TYPE,COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='user_profiles' ORDER BY ORDINAL_POSITION`)
		if err != nil {
			t.Fatal("cannot inspect profile schema")
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name, kind, key string
			if err = rows.Scan(&name, &kind, &key); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
			if name == "user_name" && (kind != "varchar" || key != "PRI") || name == "revision" && kind != "bigint" || (name == "resume_md" || name == "updated_at") && kind != "longtext" {
				t.Fatal("profile schema does not preserve source field roles")
			}
		}
		if rows.Err() != nil || !reflect.DeepEqual(names, []string{"user_name", "resume_md", "revision", "updated_at"}) {
			t.Fatal("v39 profile columns missing or reordered")
		}
	})
	t.Run("late real bootstrap and update faults rollback then retry", func(t *testing.T) {
		for _, prefix := range []string{"INSERT INTO user_profiles", "UPDATE user_profiles SET"} {
			t.Run(prefix, func(t *testing.T) {
				f := openProfileResumeFixture(t)
				var revision int64
				if strings.HasPrefix(prefix, "UPDATE") {
					revision = f.save("Demo", "CI before rollback", 0).Revision
				}
				before := f.read("Demo")
				rows := f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`)
				fault, db := openMutationFaultPool(t, prefix)
				_, err := (profiles.Repository{DB: db}).SaveResume(f.ctx, "Demo", profiles.TextInput{Text: "CI interrupted private text", Revision: revision})
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("profile fault did not occur after real SQL before commit")
				}
				if !reflect.DeepEqual(before, f.read("Demo")) || rows != f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`) {
					t.Fatal("interrupted profile transaction left partial data/revision")
				}
				db.Close()
				_, fresh := openDisposableMariaDB(t)
				fresh.SetMaxOpenConns(1)
				saved, err := (profiles.Repository{DB: fresh}).SaveResume(f.ctx, "Demo", profiles.TextInput{Text: "CI successful retry", Revision: revision})
				if err != nil || saved.Revision != revision+1 || saved.ResumeMD != "CI successful retry" {
					t.Fatal("profile retry through new pool failed")
				}
			})
		}
	})
	t.Run("two first writers or editors on one revision have one winner", func(t *testing.T) {
		for _, existing := range []bool{false, true} {
			name := "first save"
			if existing {
				name = "existing profile"
			}
			t.Run(name, func(t *testing.T) {
				// Повторяем независимые SQL-гонки: порядок выдачи блокировок после rollback различается.
				for attempt := 0; attempt < 30; attempt++ {
					t.Run(fmt.Sprintf("attempt_%02d", attempt), func(t *testing.T) { testConcurrentProfileResume(t, existing) })
				}
			})
		}
	})
}
func testConcurrentProfileResume(t *testing.T, existing bool) {
	f := openProfileResumeFixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	var revision int64
	if existing {
		revision = f.save("Demo", "CI initial profile", 0).Revision
	}
	holder, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("cannot start profile row barrier")
	}
	if existing {
		var user string
		if err = holder.QueryRowContext(ctx, `SELECT user_name FROM user_profiles WHERE user_name='Demo' FOR UPDATE`).Scan(&user); err != nil {
			holder.Rollback()
			t.Fatal("cannot hold existing profile")
		}
	} else {
		if _, err = holder.ExecContext(ctx, `INSERT INTO user_profiles(user_name) VALUES('Demo')`); err != nil {
			holder.Rollback()
			t.Fatal("cannot hold uncommitted first profile insert")
		}
	}
	var repos [2]profiles.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err = db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
			holder.Rollback()
			t.Fatal("cannot identify profile writer")
		}
		repos[i] = profiles.Repository{DB: db}
	}
	if ids[0] == ids[1] {
		holder.Rollback()
		t.Fatal("profile race requires two SQL connections")
	}
	var wg sync.WaitGroup
	var outcomes [2]error
	var replies [2]profiles.Resume
	defer func() { cancel(); holder.Rollback(); wg.Wait() }()
	inputs := []profiles.TextInput{{Text: "CI editor A", Revision: revision}, {Text: "CI editor B", Revision: revision}}
	for i := range repos {
		wg.Add(1)
		go func(i int) { defer wg.Done(); replies[i], outcomes[i] = repos[i].SaveResume(ctx, "Demo", inputs[i]) }(i)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE 'INSERT INTO user_profiles%'`, ids[0], ids[1]).Scan(&waiting); err != nil {
			t.Fatal("cannot observe profile SQL barrier")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both profile writers never reached row barrier")
		case <-tick.C:
		}
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal("cannot release profile barrier")
	}
	wg.Wait()
	winner, conflict, index := 0, 0, -1
	for i, err := range outcomes {
		if err == nil {
			winner++
			index = i
		} else if errors.Is(err, profiles.ErrConflict) {
			conflict++
		} else {
			var sqlErr *mysql.MySQLError
			if errors.As(err, &sqlErr) {
				t.Fatalf("unexpected profile race SQL outcome: number=%d state=%s", sqlErr.Number, sqlErr.SQLState)
			}
			t.Fatalf("unexpected profile race outcome: %T", err)
		}
	}
	if winner != 1 || conflict != 1 || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`) != 1 {
		t.Fatal("profile revision permitted two writers or duplicate rows")
	}
	saved := f.read("Demo")
	if saved.Revision != revision+1 || saved.ResumeMD != inputs[index].Text || !reflect.DeepEqual(saved, replies[index]) {
		t.Fatal("profile winner response did not match its own committed revision")
	}
}
