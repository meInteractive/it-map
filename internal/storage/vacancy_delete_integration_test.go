//go:build mariadb && vacancydeletes

package storage

import (
	"errors"
	"reflect"
	"testing"

	"itmap/internal/vacancies"
)

func prepareVacancyDeletion(t *testing.T, linked bool) (*vacancyCreateFixture, int64) {
	t.Helper()
	f := openVacancyCreateFixture(t)
	v := f.created(f.repo, f.input("delete"))
	id := v.ID
	if linked {
		f.linked(id)
	}
	if _, err := f.repo.SetUserFlag(f.ctx, "Demo", id, "favorite", true); err != nil {
		t.Fatal("cannot seed owned vacancy flag")
	}
	f.exec(`INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown,override_shared,revision) VALUES('Demo',?,'CI deletion private subject','CI deletion private markdown',1,1)`, id)
	return f, id
}
func (f *vacancyCreateFixture) retainedCRM() string {
	f.t.Helper()
	// При отвязке могут измениться только vacancy_id и application.updated_at.
	return f.snapshotRowSets([]string{
		`SELECT JSON_ARRAY(id,user_name,company_name,status,comment_md,created_at,recruiter_name,recruiter_phone,recruiter_email,recruiter_contact_type,application_sources) FROM company_applications WHERE company_name=? ORDER BY id`,
		`SELECT JSON_ARRAY(id,user_name,company_name,type,note,due_at,completed,created_at,completed_at,google_event_id,google_event_html_link,google_sync_status,google_sync_error) FROM company_tasks WHERE company_name=? ORDER BY id`,
		`SELECT JSON_ARRAY(h.id,h.application_id,h.from_status,h.to_status,h.changed_at) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.company_name=? ORDER BY h.id`,
	}, crudCompany)
}
func (f *vacancyCreateFixture) assertDeleted(id int64, linked bool) {
	f.t.Helper()
	if f.count(`SELECT COUNT(*) FROM company_vacancies WHERE id=?`, id) != 0 {
		f.t.Fatal("vacancy row survived hard delete")
	}
	for _, table := range []string{"vacancy_role_assignments", "vacancy_telegram_sources", "company_vacancy_user_state", "vacancy_cover_letters"} {
		if f.count("SELECT COUNT(*) FROM "+table+" WHERE vacancy_id=?", id) != 0 {
			f.t.Fatal("vacancy delete left FK child rows")
		}
	}
	if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudCompany) != 1 {
		f.t.Fatal("vacancy deletion removed company CRM anchor")
	}
	if linked && (f.count(`SELECT COUNT(*) FROM company_applications WHERE company_name=? AND vacancy_id IS NULL`, crudCompany) != 2 || f.count(`SELECT COUNT(*) FROM company_tasks WHERE company_name=? AND vacancy_id IS NULL`, crudCompany) != 2 || f.count(`SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.company_name=?`, crudCompany) != 2) {
		f.t.Fatal("hard delete lost retained application/task/history")
	}
	if _, err := f.repo.DetailByID(f.ctx, "Demo", id); !errors.Is(err, vacancies.ErrNotFound) {
		f.t.Fatal("deleted vacancy detail did not return not found")
	}
}
func TestMariaDBVacancyDeleteDisposable(t *testing.T) {
	t.Run("unreferenced vacancy deletes immediately with FK cascades", func(t *testing.T) {
		f, id := prepareVacancyDeletion(t, false)
		result, err := f.repo.DeleteManual(f.ctx, id)
		if err != nil || !result.OK || result.Archived || result.CompanyName != crudCompany || result.PreservedApplication || result.PreservedTask {
			t.Fatal("immediate vacancy deletion result changed")
		}
		f.assertDeleted(id, false)
		if _, err = f.repo.DeleteManual(f.ctx, id); !errors.Is(err, vacancies.ErrNotFound) {
			t.Fatal("repeat delete resurrected or acknowledged missing vacancy")
		}
	})
	t.Run("archive then detach delete preserves both owners CRM and history", func(t *testing.T) {
		f, id := prepareVacancyDeletion(t, true)
		before := f.detail("Demo", id)
		links, retained := f.links(id), f.retainedCRM()
		result, err := f.repo.DeleteManual(f.ctx, id)
		if err != nil || !result.OK || !result.Archived || result.CompanyName != crudCompany {
			t.Fatal("linked vacancy must archive first")
		}
		archived := f.detail("Demo", id)
		if !archived.Archived || archived.Revision == before.Revision || links != f.links(id) {
			t.Fatal("archive changed linked CRM or failed to advance visible state")
		}
		archived.Archived = before.Archived
		archived.Revision = before.Revision
		archived.UpdatedAt = before.UpdatedAt
		if !reflect.DeepEqual(before, archived) {
			t.Fatal("archive changed unrelated vacancy fields/private children")
		}
		result, err = f.repo.DeleteManual(f.ctx, id)
		if err != nil || !result.OK || result.Archived || !result.PreservedApplication || !result.PreservedTask {
			t.Fatal("second deletion must detach references and delete")
		}
		f.assertDeleted(id, true)
		if retained != f.retainedCRM() {
			t.Fatal("delete changed retained applications/tasks/history content")
		}
	})
	t.Run("real late archive detach and cascade-delete faults rollback", func(t *testing.T) {
		for _, stage := range []string{"archive", "applications", "tasks", "delete"} {
			t.Run(stage, func(t *testing.T) {
				f, id := prepareVacancyDeletion(t, true)
				prefix := "UPDATE company_vacancies SET archived"
				if stage != "archive" {
					if _, err := f.repo.DeleteManual(f.ctx, id); err != nil {
						t.Fatal("cannot prepare archived synthetic vacancy")
					}
					prefix = map[string]string{"applications": "UPDATE company_applications SET vacancy_id=NULL", "tasks": "UPDATE company_tasks SET vacancy_id=NULL", "delete": "DELETE FROM company_vacancies"}[stage]
				}
				before, other, links, retained := f.detail("Demo", id), f.detail("Nastya", id), f.links(id), f.retainedCRM()
				fault, db := openMutationFaultPool(t, prefix)
				_, err := (vacancies.Repository{DB: db}).DeleteManual(f.ctx, id)
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("delete fault did not occur after actual SQL before commit")
				}
				if !reflect.DeepEqual(before, f.detail("Demo", id)) || !reflect.DeepEqual(other, f.detail("Nastya", id)) || links != f.links(id) || retained != f.retainedCRM() {
					t.Fatal("interrupted deletion left archive/detached links/FK cascades")
				}
				if f.count(`SELECT COUNT(*) FROM vacancy_role_assignments WHERE vacancy_id=?`, id) != 1 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?`, id) != 1 || f.count(`SELECT COUNT(*) FROM vacancy_cover_letters WHERE vacancy_id=?`, id) != 1 {
					t.Fatal("rollback failed to restore deleted FK children")
				}
				db.Close()
				_, fresh := openDisposableMariaDB(t)
				result, err := (vacancies.Repository{DB: fresh}).DeleteManual(f.ctx, id)
				if err != nil || !result.OK {
					t.Fatal("fresh pool delete retry failed")
				}
				if stage == "archive" {
					if !result.Archived || !f.detail("Demo", id).Archived {
						t.Fatal("archive retry failed")
					}
				} else {
					f.assertDeleted(id, true)
					if retained != f.retainedCRM() {
						t.Fatal("retry changed retained CRM content")
					}
				}
			})
		}
	})
}
