//go:build mariadb && channelwrites

package storage

import (
	"errors"
	"itmap/internal/backup"
	"itmap/internal/channels"
	"reflect"
	"sync"
	"testing"
	"time"
)

func channelPause(t *testing.T, f *channelFixture, prefix string) (channels.Repository, <-chan struct{}, func()) {
	t.Helper()
	fault, db := openMutationFaultPool(t, prefix)
	fault.entered = make(chan struct{})
	fault.release = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(fault.release) }) }
	t.Cleanup(release)
	return channels.Repository{DB: db, CatalogSources: backup.StoredCatalogSourceURLs}, fault.entered, release
}
func awaitChannelEvent(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("channel SQL barrier was not reached")
	}
}
func waitChannelSQL(t *testing.T, f *channelFixture, prefix string) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND INFO LIKE ?", prefix+"%") > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("concurrent writer did not reach expected SQL lock")
		case <-f.ctx.Done():
			t.Fatal("channel race timed out")
		case <-ticker.C:
		}
	}
}
func awaitChannelError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("channel writer did not finish")
		return nil
	}
}
func TestMariaDBChannelRaceDisposable(t *testing.T) {
	t.Run("concurrent duplicates have one winner", func(t *testing.T) {
		for _, sameName := range []bool{false, true} {
			t.Run(map[bool]string{false: "username", true: "ASCII name"}[sameName], func(t *testing.T) {
				f := openChannelFixture(t)
				start := make(chan struct{})
				out := make(chan error, 2)
				names := []string{"CI first", "CI second"}
				users := []string{"@ci_same", "@ci_same"}
				if sameName {
					names = []string{"CI SAME", "ci same"}
					users = []string{"@ci_first", "@ci_second"}
				}
				for i := 0; i < 2; i++ {
					go func(i int) { <-start; _, e := f.repo.Create(f.ctx, channelInput(names[i], users[i])); out <- e }(i)
				}
				close(start)
				successes, duplicates := 0, 0
				for i := 0; i < 2; i++ {
					e := awaitChannelError(t, out)
					if e == nil {
						successes++
					} else {
						var validation *channels.ValidationError
						if errors.As(e, &validation) && validation.Message == "Такой независимый Telegram-канал уже добавлен." {
							duplicates++
						}
					}
				}
				if successes != 1 || duplicates != 1 || f.count("SELECT COUNT(*) FROM telegram_channels") != 1 {
					t.Fatal("duplicate create serialization failed")
				}
			})
		}
	})
	t.Run("partial updates read latest state", func(t *testing.T) {
		f := openChannelFixture(t)
		id := f.create("CI initial", "@ci_partial")
		repo, entered, release := channelPause(t, f, "UPDATE telegram_channels")
		out := make(chan error, 2)
		comment, name := "CI retained comment", "CI changed name"
		go func() { _, e := repo.Update(f.ctx, id, channels.Input{Comment: &comment}); out <- e }()
		awaitChannelEvent(t, entered)
		go func() { _, e := f.repo.Update(f.ctx, id, channels.Input{Name: &name}); out <- e }()
		waitChannelSQL(t, f, "SELECT GET_LOCK(CONCAT(")
		release()
		for i := 0; i < 2; i++ {
			if e := awaitChannelError(t, out); e != nil {
				t.Fatal("partial concurrent update failed")
			}
		}
		got := f.list()
		if len(got) != 1 || got[0].Name != name || got[0].Comment != comment {
			t.Fatal("partial concurrent update lost newer fields")
		}
	})
	t.Run("two source deletions cannot orphan channel vacancy", func(t *testing.T) {
		f := openChannelFixture(t)
		a, b := f.create("CI source A", "@ci_source_a"), f.create("CI source B", "@ci_source_b")
		v := f.vacancy("channel")
		f.link(v, a)
		f.link(v, b)
		repo, entered, release := channelPause(t, f, "DELETE FROM telegram_channels")
		one, two := make(chan error, 1), make(chan error, 1)
		go func() { _, e := repo.Delete(f.ctx, a); one <- e }()
		awaitChannelEvent(t, entered)
		go func() { _, e := f.repo.Delete(f.ctx, b); two <- e }()
		waitChannelSQL(t, f, "SELECT GET_LOCK(CONCAT(")
		release()
		if e := awaitChannelError(t, one); e != nil {
			t.Fatal("first source delete failed")
		}
		if !errors.Is(awaitChannelError(t, two), channels.ErrSoleSource) || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?", v) != 1 || f.count("SELECT COUNT(*) FROM telegram_channels WHERE id=?", b) != 1 {
			t.Fatal("concurrent delete removed last source")
		}
	})
	t.Run("owner writer fences delete", func(t *testing.T) {
		f := openChannelFixture(t)
		a := f.create("CI owner", "@ci_owner")
		v := f.vacancy("company")
		f.link(v, a)
		before := f.list()
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal("cannot begin owner writer")
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(f.ctx, "UPDATE company_vacancies SET entity_type='channel' WHERE id=?", v); e != nil {
			t.Fatal("cannot stage owner change")
		}
		out := make(chan error, 1)
		go func() { _, e := f.repo.Delete(f.ctx, a); out <- e }()
		waitChannelSQL(t, f, "SELECT id FROM company_vacancies WHERE id=")
		if e = tx.Commit(); e != nil {
			t.Fatal("cannot commit owner change")
		}
		if !errors.Is(awaitChannelError(t, out), channels.ErrSoleSource) || !reflect.DeepEqual(before, f.list()) {
			t.Fatal("delete ignored concurrent owner change")
		}
	})
	t.Run("new FK link during discovery forces retry", func(t *testing.T) {
		f := openChannelFixture(t)
		a := f.create("CI discovery", "@ci_discovery")
		v1, v2 := f.vacancy("company"), f.vacancy("channel")
		f.link(v1, a)
		repo, entered, release := channelPause(t, f, "SELECT id FROM company_vacancies WHERE id=")
		out := make(chan error, 1)
		go func() { _, e := repo.Delete(f.ctx, a); out <- e }()
		awaitChannelEvent(t, entered)
		f.link(v2, a)
		release()
		if !errors.Is(awaitChannelError(t, out), channels.ErrSoleSource) || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE channel_id=?", a) != 2 {
			t.Fatal("discovery retry lost new source or ignored new vacancy")
		}
	})
}
