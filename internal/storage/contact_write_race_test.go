//go:build mariadb && contactwrites

package storage

import (
	"itmap/internal/backup"
	"itmap/internal/contacts"
	"sync"
	"testing"
	"time"
)

func contactPause(t *testing.T, f *contactFixture, prefix string) (contacts.Repository, <-chan struct{}, func()) {
	t.Helper()
	fault, db := openMutationFaultPool(t, prefix)
	fault.entered = make(chan struct{})
	fault.release = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(fault.release) }) }
	t.Cleanup(release)
	return contacts.Repository{DB: db, CatalogNames: backup.StoredCatalogNames}, fault.entered, release
}
func contactEvent(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("contact SQL barrier not reached")
	}
}
func contactDone(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("contact writer did not finish")
		return nil
	}
}
func contactBlocked(t *testing.T, f *contactFixture, prefix string) {
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
			t.Fatal("contact writer did not reach expected lock")
		case <-f.ctx.Done():
			t.Fatal("contact race timed out")
		case <-ticker.C:
		}
	}
}
func TestMariaDBContactRaceDisposable(t *testing.T) {
	t.Run("concurrent creates preserve all three unique slots", func(t *testing.T) {
		f := openContactFixture(t)
		start := make(chan struct{})
		out := make(chan error, 5)
		for _, p := range []int{1, 2, 3, 1, 2} {
			go func(p int) {
				<-start
				_, e := f.repo.Create(f.ctx, f.source.Company, contactRaw(p, "CI competing"))
				out <- e
			}(p)
		}
		close(start)
		success := 0
		for i := 0; i < 5; i++ {
			e := contactDone(t, out)
			if e == nil {
				success++
			} else if _, ok := e.(*contacts.ValidationError); !ok {
				t.Fatal("unexpected concurrent create failure")
			}
		}
		items := f.list(f.source.Company)
		if success != 3 || len(items) != 3 || items[0].Position != 1 || items[1].Position != 2 || items[2].Position != 3 {
			t.Fatal("contact concurrency exceeded capacity or lost slot")
		}
	})
	t.Run("concurrent moves have one winner and preserve loser", func(t *testing.T) {
		f := openContactFixture(t)
		a, b := f.create(f.source.Company, 1), f.create(f.source.Company, 2)
		start := make(chan struct{})
		out := make(chan error, 2)
		for _, id := range []int64{a.ID, b.ID} {
			go func(id int64) { <-start; _, e := f.repo.Update(f.ctx, id, contactRaw(3, "CI moved")); out <- e }(id)
		}
		close(start)
		success, duplicate := 0, 0
		for i := 0; i < 2; i++ {
			e := contactDone(t, out)
			if e == nil {
				success++
			} else if v, ok := e.(*contacts.ValidationError); ok && v.Message == "Эта позиция контакта уже занята." {
				duplicate++
			}
		}
		items := f.list(f.source.Company)
		if success != 1 || duplicate != 1 || len(items) != 2 || items[0].FullName != "CI Contact" || items[1].Position != 3 {
			t.Fatal("contact move overwrote losing row")
		}
	})
	t.Run("delete releases slot only after commit", func(t *testing.T) {
		f := openContactFixture(t)
		a := f.create(f.source.Company, 1)
		repo, entered, release := contactPause(t, f, "DELETE FROM hiring_contacts")
		one, two := make(chan error, 1), make(chan error, 1)
		go func() { _, e := repo.Delete(f.ctx, a.ID); one <- e }()
		contactEvent(t, entered)
		go func() { _, e := f.repo.Create(f.ctx, f.source.Company, contactRaw(1, "CI replacement")); two <- e }()
		contactBlocked(t, f, "INSERT INTO company_crm")
		release()
		if contactDone(t, one) != nil || contactDone(t, two) != nil {
			t.Fatal("delete/create slot serialization failed")
		}
		items := f.list(f.source.Company)
		if len(items) != 1 || items[0].ID == a.ID || items[0].FullName != "CI replacement" {
			t.Fatal("slot reuse lost replacement")
		}
	})
	t.Run("owner replacement during discovery is rechecked", func(t *testing.T) {
		f := openContactFixture(t)
		a := f.create(f.source.Company, 1)
		f.exec("INSERT INTO company_crm(company_name)VALUES(?)", f.source.Agency)
		repo, entered, release := contactPause(t, f, "SELECT company_name FROM hiring_contacts WHERE id=")
		out := make(chan error, 1)
		go func() { _, e := repo.Update(f.ctx, a.ID, contactRaw(2, "CI new owner")); out <- e }()
		contactEvent(t, entered)
		f.exec("UPDATE hiring_contacts SET company_name=? WHERE id=?", f.source.Agency, a.ID)
		release()
		if contactDone(t, out) != nil {
			t.Fatal("owner recheck retry failed")
		}
		items := f.list(f.source.Agency)
		if len(f.list(f.source.Company)) != 0 || len(items) != 1 || items[0].ID != a.ID || items[0].Position != 2 {
			t.Fatal("owner discovery wrote stale company")
		}
	})
}
