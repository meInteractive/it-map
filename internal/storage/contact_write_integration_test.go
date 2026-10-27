//go:build mariadb && contactwrites

package storage

import (
	"encoding/json"
	"errors"
	"itmap/internal/backup"
	"itmap/internal/contacts"
	"reflect"
	"testing"
)

func TestMariaDBContactCRUDDisposable(t *testing.T) {
	t.Run("source scenario and owner isolation", func(t *testing.T) { f := openContactFixture(t); runContactScenario(t, f, nil) })
	t.Run("reads do not initialize rows and one connection supports readback", func(t *testing.T) {
		f := openContactFixture(t)
		fault, db := openMutationFaultPool(t, "")
		repo := contacts.Repository{DB: db, CatalogNames: backup.StoredCatalogNames}
		out, e := repo.List(f.ctx, f.source.Company)
		fault.mu.Lock()
		writes := fault.writes
		fault.mu.Unlock()
		if e != nil || out == nil || len(out) != 0 || writes != 0 || f.count("SELECT COUNT(*) FROM company_crm") != 0 {
			t.Fatal("contact GET wrote rows or changed defaults")
		}
		f.db.SetMaxOpenConns(1)
		c := f.create(f.source.Company, 1)
		if _, e = f.repo.Update(f.ctx, c.ID, contactRaw(1, "CI unchanged")); e != nil {
			t.Fatal("one-connection update/readback failed")
		}
		if _, e = f.repo.Delete(f.ctx, c.ID); e != nil {
			t.Fatal("one-connection delete/readback failed")
		}
	})
	t.Run("actual late writes and readback rollback including anchor", func(t *testing.T) {
		for _, prefix := range []string{"INSERT INTO company_crm", "INSERT INTO hiring_contacts", "UPDATE hiring_contacts", "DELETE FROM hiring_contacts", "SELECT id,company_name,position,full_name"} {
			t.Run(prefix, func(t *testing.T) {
				f := openContactFixture(t)
				var id int64
				if prefix == "UPDATE hiring_contacts" || prefix == "DELETE FROM hiring_contacts" {
					id = f.create(f.source.Company, 1).ID
				}
				before := f.list(f.source.Company)
				anchor := f.count("SELECT COUNT(*) FROM company_crm")
				fault, db := openMutationFaultPool(t, prefix)
				if prefix == "SELECT id,company_name,position,full_name" {
					fault.suffix = "ORDER BY position"
				}
				repo := contacts.Repository{DB: db, CatalogNames: backup.StoredCatalogNames}
				var e error
				switch prefix {
				case "UPDATE hiring_contacts":
					_, e = repo.Update(f.ctx, id, contactRaw(2, "CI rolled back"))
				case "DELETE FROM hiring_contacts":
					_, e = repo.Delete(f.ctx, id)
				default:
					_, e = repo.Create(f.ctx, f.source.Company, contactRaw(1, "CI rolled back"))
				}
				fault.mu.Lock()
				fired, writes, queries, commits := fault.fired, fault.writes, fault.queries, fault.commits
				fault.mu.Unlock()
				if !errors.Is(e, errMutationFault) || !fired || writes+queries != 1 || commits != 0 {
					t.Fatal("contact fault was not after real SQL before commit")
				}
				if !reflect.DeepEqual(before, f.list(f.source.Company)) || f.count("SELECT COUNT(*) FROM company_crm") != anchor {
					t.Fatal("late contact failure left changed rows or anchor")
				}
				db.Close()
				if id != 0 {
					if _, e = f.repo.Update(f.ctx, id, contactRaw(1, "CI retry")); e != nil {
						t.Fatal("contact update retry failed")
					}
				} else {
					f.create(f.source.Company, 1)
				}
			})
		}
	})
	t.Run("schema enforces three slots and CRM FK", func(t *testing.T) {
		f := openContactFixture(t)
		f.create(f.source.Company, 1)
		for _, q := range []string{"INSERT INTO hiring_contacts(company_name,position,full_name)VALUES('CI unknown',1,'CI FK')", "INSERT INTO hiring_contacts(company_name,position,full_name)VALUES('CI Contact Employer',4,'CI position')", "INSERT INTO hiring_contacts(company_name,position,full_name)VALUES('CI Contact Employer',1,'CI duplicate')"} {
			if _, e := f.db.ExecContext(f.ctx, q); e == nil {
				t.Fatal("contact FK/check/unique constraint not enforced")
			}
		}
	})
}
func runContactScenario(t *testing.T, f *contactFixture, call func(string, int64, string, json.RawMessage) (int, []byte)) {
	t.Helper()
	ids := map[int64]int64{}
	for _, step := range f.source.Steps {
		id := int64(0)
		if step.ID != nil {
			id = *step.ID
			if actual, ok := ids[id]; ok {
				id = actual
			}
		}
		status := 200
		var body []byte
		if call != nil {
			status, body = call(step.Method, id, step.Owner, step.Payload)
		} else {
			var in contacts.Input
			if step.Method == "POST" || step.Method == "PUT" {
				if json.Unmarshal(step.Payload, &in) != nil {
					t.Fatal("invalid source contact payload")
				}
			}
			var result any
			var e error
			switch step.Method {
			case "GET":
				var items []contacts.Contact
				items, e = f.repo.List(f.ctx, step.Owner)
				result = map[string]any{"companyName": step.Owner, "limit": 3, "contacts": items}
			case "POST":
				result, e = f.repo.Create(f.ctx, step.Owner, in)
				status = 201
			case "PUT":
				result, e = f.repo.Update(f.ctx, id, in)
			case "DELETE":
				result, e = f.repo.Delete(f.ctx, id)
			}
			if e != nil {
				status = 400
				message := e.Error()
				if errors.Is(e, contacts.ErrNotFound) {
					status = 404
					message = "Контакт не найден."
				} else if errors.Is(e, contacts.ErrCompanyNotFound) {
					status = 404
					message = "Компания или агентство не найдены."
				}
				body, _ = json.Marshal(map[string]string{"error": message})
			} else {
				body, _ = json.Marshal(result)
			}
		}
		if status != step.Status {
			t.Fatalf("%s: status %d expected %d", step.Name, status, step.Status)
		}
		if status >= 400 {
			var reply struct{ Error string }
			if json.Unmarshal(body, &reply) != nil || reply.Error != step.Error {
				t.Fatalf("%s: source contact error differs", step.Name)
			}
			continue
		}
		var got, want map[string]any
		if json.Unmarshal(body, &got) != nil || json.Unmarshal(step.Result, &want) != nil {
			t.Fatal("invalid source contact reply")
		}
		gl, wl := got["contacts"].([]any), want["contacts"].([]any)
		if len(gl) != len(wl) {
			t.Fatal("contact count differs")
		}
		for i, raw := range wl {
			w, g := raw.(map[string]any), gl[i].(map[string]any)
			sourceID, actual := int64(w["id"].(float64)), int64(g["id"].(float64))
			if known, ok := ids[sourceID]; ok && known != actual {
				t.Fatal("contact ID mapping changed")
			}
			ids[sourceID] = actual
			w["id"] = float64(actual)
			var created, updated string
			if e := f.db.QueryRowContext(f.ctx, "SELECT created_at,updated_at FROM hiring_contacts WHERE id=?", actual).Scan(&created, &updated); e != nil || g["createdAt"] != created || g["updatedAt"] != updated {
				t.Fatal("contact returned clocks differ from SQL")
			}
			delete(g, "createdAt")
			delete(g, "updatedAt")
			delete(w, "createdAt")
			delete(w, "updatedAt")
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: normalized contact source reply differs", step.Name)
		}
	}
	if f.count("SELECT COUNT(*) FROM hiring_contacts") != 2 {
		t.Fatal("contact scenario lost owner isolation")
	}
	var catalog string
	if e := f.db.QueryRowContext(f.ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalog); e != nil || catalog != string(f.source.Catalog) {
		t.Fatal("contact writes changed catalog")
	}
}
