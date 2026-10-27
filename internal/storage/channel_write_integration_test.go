//go:build mariadb && channelwrites

package storage

import (
	"encoding/json"
	"errors"
	"itmap/internal/backup"
	"itmap/internal/channels"
	"reflect"
	"testing"
)

func TestMariaDBChannelCRUDDisposable(t *testing.T) {
	t.Run("source scenario and partial fallback", func(t *testing.T) { f := openChannelFixture(t); runChannelScenario(t, f, nil); f.assertCatalog() })
	t.Run("source ASCII NOCASE ordering with binary unique names", func(t *testing.T) {
		f := openChannelFixture(t)
		for i, name := range f.source.OrderNames {
			f.exec("INSERT INTO telegram_channels(id,name,url,normalized_username,username)VALUES(?,?,?,?,?)", 100+i, name, "https://t.me/ci_order", "ci_order_"+string(rune('a'+i)), "")
		}
		got := f.list()
		if len(got) != 12 {
			t.Fatal("order corpus missing")
		}
		for i, c := range got {
			if c.ID != f.source.Ordered[i].ID || c.Name != f.source.Ordered[i].Name {
				t.Fatal("SQL reader order differs from SQLite source")
			}
		}
	})
	t.Run("one connection write readback and no-op update", func(t *testing.T) {
		f := openChannelFixture(t)
		f.db.SetMaxOpenConns(1)
		id := f.create("CI one", "@ci_single")
		if _, e := f.repo.Update(f.ctx, id, channels.Input{}); e != nil {
			t.Fatal("no-op update/readback failed with one connection")
		}
		if _, e := f.repo.Delete(f.ctx, id); e != nil {
			t.Fatal("delete failed with one connection")
		}
	})
	t.Run("late actual write and readback faults roll back", func(t *testing.T) {
		for _, prefix := range []string{"INSERT INTO telegram_channels", "UPDATE telegram_channels", "DELETE FROM telegram_channels", "SELECT c.id"} {
			t.Run(prefix, func(t *testing.T) {
				f := openChannelFixture(t)
				id := f.create("CI before", "@ci_before")
				v := f.vacancy("company")
				f.link(v, id)
				before := f.list()
				fault, db := openMutationFaultPool(t, prefix)
				repo := channels.Repository{DB: db, CatalogSources: backup.StoredCatalogSourceURLs}
				var e error
				switch prefix {
				case "INSERT INTO telegram_channels":
					_, e = repo.Create(f.ctx, channelInput("CI failed", "@ci_failed"))
				case "DELETE FROM telegram_channels":
					_, e = repo.Delete(f.ctx, id)
				default:
					s := "CI modified"
					_, e = repo.Update(f.ctx, id, channels.Input{Comment: &s})
				}
				fault.mu.Lock()
				fired, writes, queries, commits := fault.fired, fault.writes, fault.queries, fault.commits
				fault.mu.Unlock()
				if !errors.Is(e, errMutationFault) || !fired || writes+queries != 1 || commits != 0 {
					t.Fatal("fault did not follow actual SQL before commit")
				}
				if !reflect.DeepEqual(before, f.list()) || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=? AND channel_id=?", v, id) != 1 {
					t.Fatal("late failure changed channel or FK sources")
				}
				f.assertCatalog()
				db.Close()
				s := "CI retry"
				if _, e = f.repo.Update(f.ctx, id, channels.Input{Comment: &s}); e != nil {
					t.Fatal("healthy retry failed after rollback")
				}
			})
		}
	})
	t.Run("failed unlock discards physical session after successful commit", func(t *testing.T) {
		f := openChannelFixture(t)
		fault, db := openMutationFaultPool(t, "BEFORE:SELECT RELEASE_LOCK")
		repo := channels.Repository{DB: db, CatalogSources: backup.StoredCatalogSourceURLs}
		v, e := repo.Create(f.ctx, channelInput("CI release", "@ci_release"))
		if e != nil || !v.OK || f.count("SELECT COUNT(*) FROM telegram_channels WHERE id=?", v.ChannelID) != 1 {
			t.Fatal("post-commit unlock fault changed success")
		}
		fault.mu.Lock()
		fired, commits := fault.fired, fault.commits
		fault.mu.Unlock()
		if !fired || commits != 1 || db.Stats().OpenConnections != 0 {
			t.Fatal("failed unlock returned locked session to pool")
		}
		var free int
		if e = f.db.QueryRowContext(f.ctx, "SELECT IS_FREE_LOCK(CONCAT('itmap:channel:',LEFT(SHA2(DATABASE(),256),32)))").Scan(&free); e != nil || free != 1 {
			t.Fatal("discarded session retained channel lock")
		}
	})
}

// runChannelScenario uses the unchanged Node repository as the expected result.
// MariaDB allocates different IDs and clocks; map IDs, compare every other field,
// and separately prove returned clocks equal their persisted values.
func runChannelScenario(t *testing.T, f *channelFixture, call func(string, int64, json.RawMessage) (int, []byte)) {
	t.Helper()
	ids := map[int64]int64{}
	var first, second, v int64
	for _, step := range f.source.Steps {
		switch step.Name {
		case "sole source delete is conflict":
			v = f.vacancy("channel")
			f.link(v, first)
		case "alternative source allows detach":
			f.link(v, second)
		case "company vacancy may lose channel provenance":
			f.exec("UPDATE company_vacancies SET entity_type='company' WHERE id=?", v)
		}
		id := int64(0)
		if step.ID != nil {
			id = *step.ID
			if actual, ok := ids[id]; ok {
				id = actual
			}
		}
		var body []byte
		status := 200
		if call != nil {
			status, body = call(step.Method, id, step.Payload)
		} else {
			var input channels.Input
			if step.Method != "DELETE" && json.Unmarshal(step.Payload, &input) != nil {
				t.Fatal("invalid oracle payload")
			}
			var result any
			var e error
			switch step.Method {
			case "POST":
				result, e = f.repo.Create(f.ctx, input)
				status = 201
			case "PUT":
				result, e = f.repo.Update(f.ctx, id, input)
			case "DELETE":
				result, e = f.repo.Delete(f.ctx, id)
			}
			if e != nil {
				status = 400
				if errors.Is(e, channels.ErrNotFound) {
					status = 404
				} else if errors.Is(e, channels.ErrSoleSource) {
					status = 409
				}
				body, _ = json.Marshal(map[string]string{"error": e.Error()})
			} else {
				body, _ = json.Marshal(result)
			}
		}
		if status != step.Status {
			t.Fatalf("%s: status %d, expected %d", step.Name, status, step.Status)
		}
		if status >= 400 {
			var reply struct{ Error string }
			if json.Unmarshal(body, &reply) != nil {
				t.Fatal("invalid error JSON")
			}
			expected := step.Error
			if status == 404 {
				expected = "Канал не найден."
			}
			if call == nil && status == 404 {
				expected = channels.ErrNotFound.Error()
			}
			if reply.Error != expected {
				t.Fatalf("%s: fixed source error differs", step.Name)
			}
			continue
		}
		var got, want map[string]any
		if json.Unmarshal(body, &got) != nil || json.Unmarshal(step.Result, &want) != nil {
			t.Fatal("invalid success JSON")
		}
		if step.Method == "POST" {
			sourceID := int64(want["channelId"].(float64))
			actual := int64(got["channelId"].(float64))
			ids[sourceID] = actual
			if first == 0 {
				first = actual
			} else {
				second = actual
			}
		}
		rewriteChannelReply(want, ids)
		if list, ok := got["channels"].([]any); ok {
			for _, raw := range list {
				c := raw.(map[string]any)
				var created, updated string
				if e := f.db.QueryRowContext(f.ctx, "SELECT created_at,updated_at FROM telegram_channels WHERE id=?", int64(c["id"].(float64))).Scan(&created, &updated); e != nil || created != c["createdAt"] || updated != c["updatedAt"] {
					t.Fatal("returned channel clocks differ from SQL")
				}
			}
		}
		stripChannelClocks(got)
		stripChannelClocks(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: normalized source result differs\ngot %s\nwant %s", step.Name, mustChannelJSON(got), mustChannelJSON(want))
		}
	}
	if f.count("SELECT COUNT(*) FROM telegram_channels") != 0 || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources") != 0 || f.count("SELECT COUNT(*) FROM company_vacancies") != 1 {
		t.Fatal("source delete scenario lost vacancy or leaked source")
	}
}
func rewriteChannelReply(v map[string]any, ids map[int64]int64) {
	if id, ok := v["channelId"].(float64); ok {
		v["channelId"] = float64(ids[int64(id)])
	}
	if list, ok := v["channels"].([]any); ok {
		for _, raw := range list {
			c := raw.(map[string]any)
			c["id"] = float64(ids[int64(c["id"].(float64))])
		}
	}
}
func stripChannelClocks(v map[string]any) {
	if list, ok := v["channels"].([]any); ok {
		for _, raw := range list {
			c := raw.(map[string]any)
			delete(c, "createdAt")
			delete(c, "updatedAt")
		}
	}
}
func mustChannelJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
