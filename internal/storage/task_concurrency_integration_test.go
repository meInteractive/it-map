//go:build mariadb && taskwrites

package storage

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"itmap/internal/tasks"
)

func testTaskConcurrentEdits(t *testing.T) {
	f := openTaskFixture(t)
	id := f.create()
	before := f.row(id)
	var repos [2]tasks.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err := db.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&ids[i]); err != nil {
			t.Fatal("cannot identify task writer connection")
		}
		repos[i] = tasks.Repository{DB: db}
	}
	if ids[0] == ids[1] {
		t.Fatal("concurrent task edits require distinct server connections")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	holder, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("cannot begin task row barrier")
	}
	var locked int64
	if err := holder.QueryRowContext(ctx, "SELECT id FROM company_tasks WHERE id=? AND user_name=? FOR UPDATE", id, taskUser).Scan(&locked); err != nil {
		holder.Rollback()
		t.Fatal("cannot hold synthetic task row")
	}
	var wg sync.WaitGroup
	var outcomes [2]error
	defer func() { cancel(); holder.Rollback(); wg.Wait() }()
	for i := range repos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 0 {
				outcomes[i] = repos[i].SetCompleted(ctx, taskUser, id, true)
			} else {
				_, outcomes[i] = repos[i].Reschedule(ctx, taskUser, id, "2026-11-02T00:15:00+03:00")
			}
		}(i)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST
   WHERE ID IN (?,?) AND COMMAND IN ('Query','Execute') AND INFO LIKE '%FROM company_tasks WHERE user_name=%FOR UPDATE%'`, ids[0], ids[1]).Scan(&waiting)
		if err != nil {
			t.Fatal("cannot observe both task writers at the row barrier")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both task writers did not reach the real SQL barrier")
		case <-ticker.C:
		}
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal("cannot release task row barrier")
	}
	wg.Wait()
	if outcomes[0] != nil || outcomes[1] != nil {
		t.Fatal("concurrent completion/reschedule failed")
	}
	after := f.row(id)
	expected := before
	expected.DueAt = "2026-11-01T21:15:00.000Z"
	expected.Completed = true
	expected.CompletedAt = after.CompletedAt
	if after.CompletedAt == nil || !reflect.DeepEqual(expected, after) {
		t.Fatal("concurrent edits lost completion, deadline or unchanged task metadata")
	}
}
