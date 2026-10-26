//go:build mariadb && vacancyedits

package storage

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

func (f *vacancyCreateFixture) detail(user string, id int64) vacancies.Detail {
	f.t.Helper()
	v, err := f.repo.DetailByID(f.ctx, user, id)
	if err != nil {
		f.t.Fatalf("owned vacancy detail failed: %T", err)
	}
	return v.Vacancy
}
func (f *vacancyCreateFixture) editInput(v vacancies.Detail) vacancies.UpdateRequest {
	in := f.input("edit")
	in.URL = v.URL
	in.CompanyName = v.CompanyName
	in.EntityType = v.EntityType
	in.Title = "CI edited vacancy"
	in.RoleIDs = []int64{f.leaf}
	return vacancies.UpdateRequest{Input: in, Revision: v.Revision}
}
func (f *vacancyCreateFixture) edit(id int64, in vacancies.UpdateRequest) vacancies.Detail {
	f.t.Helper()
	if _, err := f.repo.UpdateManual(f.ctx, "Demo", id, in); err != nil {
		f.t.Fatalf("manual vacancy update failed: %T", err)
	}
	return f.detail("Demo", id)
}
func (f *vacancyCreateFixture) linked(id int64) {
	f.t.Helper()
	for _, user := range []string{"Demo", "Nastya"} {
		app := f.insert(`INSERT INTO company_applications(user_name,company_name,vacancy_id,status,comment_md) VALUES(?,?,?,'Откликнулся','CI linked comment')`, user, crudCompany, id)
		f.insert(`INSERT INTO company_application_status_history(application_id,from_status,to_status) VALUES(?,'','Откликнулся')`, app)
		f.insert(`INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at) VALUES(?,?,?,'call','CI linked task','2026-10-26T12:00:00Z')`, user, crudCompany, id)
	}
}
func (f *vacancyCreateFixture) links(id int64) string {
	f.t.Helper()
	var result string
	f.scan(`SELECT JSON_ARRAY(
 (SELECT JSON_ARRAYAGG(JSON_ARRAY(id,user_name,company_name,vacancy_id,status,comment_md,updated_at) ORDER BY id) FROM company_applications WHERE vacancy_id=?),
 (SELECT JSON_ARRAYAGG(JSON_ARRAY(id,user_name,company_name,vacancy_id,type,note,due_at,completed,created_at,completed_at) ORDER BY id) FROM company_tasks WHERE vacancy_id=?),
 (SELECT JSON_ARRAYAGG(JSON_ARRAY(h.id,h.application_id,h.from_status,h.to_status,h.changed_at) ORDER BY h.id) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.vacancy_id=?))`, []any{id, id, id}, &result)
	return result
}
func TestMariaDBVacancyEditDisposable(t *testing.T) {
	t.Run("whole form CAS flags roles letters and source presence", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		created := f.created(f.repo, f.input("edit"))
		id := created.ID
		if _, err := f.repo.SetUserFlag(f.ctx, "Demo", id, "favorite", true); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SetUserFlag(f.ctx, "Demo", id, "hidden", true); err != nil {
			t.Fatal(err)
		}
		before := f.detail("Demo", id)
		input := f.editInput(before)
		score := int64(72)
		input.Relevance = &score
		input.RoleIDs = []int64{f.otherLeaf, f.otherLeaf}
		f.db.SetMaxOpenConns(1)
		got := f.edit(id, input)
		f.assertLockFree()
		if got.Revision == before.Revision || got.Title != input.Title || !got.Favorite || !got.Hidden || got.Relevance == nil || *got.Relevance != 72 || len(got.Roles) != 1 || got.Roles[0].ID != f.otherLeaf || len(got.TelegramSources) != 1 || got.CoverLetterMD != before.CoverLetterMD {
			t.Fatal("whole-form edit lost CAS/flags/role/source/shared letter")
		}
		if _, err := f.repo.UpdateManual(f.ctx, "Demo", id, input); !errors.Is(err, vacancies.ErrVersionConflict) {
			t.Fatal("stale revision accepted")
		}
		if !reflect.DeepEqual(got, f.detail("Demo", id)) {
			t.Fatal("stale write changed vacancy")
		}
		missing := f.editInput(got)
		missing.Revision = ""
		if _, err := f.repo.UpdateManual(f.ctx, "Demo", id, missing); !errors.Is(err, vacancies.ErrRevisionRequired) {
			t.Fatal("missing revision accepted")
		}
		personal := f.editInput(got)
		personal.RoleIDs = []int64{f.otherLeaf}
		personal.LetterProvided = true
		personal.CoverLetterScope = "personal"
		personal.CoverLetterSubject = "CI personal subject"
		personal.CoverLetterMD = "CI personal markdown"
		got = f.edit(id, personal)
		if got.CoverLetterScope != "personal" || got.CoverLetterMD != personal.CoverLetterMD {
			t.Fatal("personal letter did not override shared")
		}
		other := f.detail("Nastya", id)
		if other.CoverLetterScope != "shared" || other.CoverLetterMD != before.CoverLetterMD {
			t.Fatal("personal letter leaked to another owner")
		}
		preserve := f.editInput(got)
		preserve.Title = "CI unrelated edit"
		got = f.edit(id, preserve)
		other = f.detail("Nastya", id)
		if got.CoverLetterMD != personal.CoverLetterMD || other.CoverLetterMD != before.CoverLetterMD {
			t.Fatal("omitted letter edit copied private text to shared record")
		}
		shared := f.editInput(got)
		shared.LetterProvided = true
		shared.CoverLetterScope = "shared"
		shared.CoverLetterSubject = "CI new shared subject"
		shared.CoverLetterMD = "CI new shared markdown"
		got = f.edit(id, shared)
		if got.CoverLetterMD != personal.CoverLetterMD || f.detail("Nastya", id).CoverLetterMD != shared.CoverLetterMD {
			t.Fatal("shared letter update overwrote personal override")
		}
		clear := f.editInput(got)
		clear.LetterProvided = true
		clear.CoverLetterScope = "personal"
		clear.CoverLetterSubject = ""
		clear.CoverLetterMD = ""
		got = f.edit(id, clear)
		if got.CoverLetterScope != "shared" || got.CoverLetterMD != shared.CoverLetterMD || f.count(`SELECT COUNT(*) FROM vacancy_cover_letters WHERE user_name='Demo' AND vacancy_id=?`, id) != 0 {
			t.Fatal("personal letter clear failed to restore shared fallback")
		}
		detach := f.editInput(got)
		detach.TelegramProvided = true
		detach.TelegramChannelID = nil
		detach.TelegramMessageURL = ""
		got = f.edit(id, detach)
		if len(got.TelegramSources) != 0 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?`, id) != 0 {
			t.Fatal("explicit source detach was ignored")
		}
		f.assertLockFree()
	})
	t.Run("owner move bootstraps FK parent and preserves linked CRM", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		created := f.created(f.repo, f.input("owner"))
		id := created.ID
		f.linked(id)
		before := f.detail("Demo", id)
		input := f.editInput(before)
		input.CompanyName = crudMoveCompany
		if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudMoveCompany) != 0 {
			t.Fatal("move requires fresh known catalog owner")
		}
		got := f.edit(id, input)
		if got.CompanyName != crudMoveCompany || f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudMoveCompany) != 1 || f.count(`SELECT COUNT(*) FROM company_applications WHERE vacancy_id=? AND company_name=?`, id, crudMoveCompany) != 2 || f.count(`SELECT COUNT(*) FROM company_tasks WHERE vacancy_id=? AND company_name=?`, id, crudMoveCompany) != 2 || f.count(`SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.vacancy_id=?`, id) != 2 {
			t.Fatal("owner move lost bootstrap/application/task/history links")
		}
		for _, user := range []string{"Demo", "Nastya"} {
			v := f.detail(user, id)
			if v.CompanyName != crudMoveCompany || v.ApplicationID == nil || v.ApplicationStatus != "Откликнулся" {
				t.Fatal("owner move lost another user's linked application projection")
			}
		}
	})
	t.Run("late SQL edits rollback fields letter source roles relevance and links", func(t *testing.T) {
		for _, prefix := range []string{"UPDATE company_vacancies SET", "DELETE FROM vacancy_role_assignments", "INSERT INTO vacancy_role_assignments", "INSERT INTO vacancy_cover_letters", "UPDATE company_applications SET company_name", "UPDATE company_tasks SET company_name", "UPDATE vacancy_telegram_sources SET", "UPDATE company_vacancy_user_state SET"} {
			t.Run(prefix, func(t *testing.T) {
				f := openVacancyCreateFixture(t)
				created := f.created(f.repo, f.input("rollback-edit"))
				id := created.ID
				f.linked(id)
				f.exec(`INSERT INTO company_crm(company_name) VALUES(?)`, crudMoveCompany)
				before, other, links := f.detail("Demo", id), f.detail("Nastya", id), f.links(id)
				input := f.editInput(before)
				input.CompanyName = crudMoveCompany
				input.RoleIDs = []int64{f.otherLeaf}
				input.LetterProvided = true
				input.CoverLetterScope = "personal"
				input.CoverLetterSubject = "CI rollback personal"
				input.CoverLetterMD = "CI rollback private markdown"
				input.TelegramProvided = true
				input.TelegramMessageURL = "https://t.me/ci_go088_channel/124"
				score := int64(81)
				input.Relevance = &score
				fault, db := openMutationFaultPool(t, prefix)
				db.SetMaxOpenConns(1)
				_, err := (vacancies.Repository{DB: db}).UpdateManual(f.ctx, "Demo", id, input)
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("edit fault did not fire after real SQL before commit")
				}
				if !reflect.DeepEqual(before, f.detail("Demo", id)) || !reflect.DeepEqual(other, f.detail("Nastya", id)) || links != f.links(id) {
					t.Fatal("failed edit left changed vacancy, private rows or linked CRM")
				}
				f.assertLockFree()
				db.Close()
				_, fresh := openDisposableMariaDB(t)
				fresh.SetMaxOpenConns(1)
				if _, err = (vacancies.Repository{DB: fresh}).UpdateManual(f.ctx, "Demo", id, input); err != nil {
					t.Fatalf("fresh pool edit retry failed: %T", err)
				}
				f.assertLockFree()
			})
		}
	})
	t.Run("new owner bootstrap also rolls back after late linked-task failure", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		created := f.created(f.repo, f.input("move-rollback"))
		id := created.ID
		f.linked(id)
		before, links := f.detail("Demo", id), f.links(id)
		input := f.editInput(before)
		input.CompanyName = crudMoveCompany
		fault, db := openMutationFaultPool(t, "UPDATE company_tasks SET company_name")
		_, err := (vacancies.Repository{DB: db}).UpdateManual(f.ctx, "Demo", id, input)
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !errors.Is(err, errMutationFault) || !fired {
			t.Fatal("move interruption did not reach linked task update")
		}
		if f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, crudMoveCompany) != 0 || !reflect.DeepEqual(before, f.detail("Demo", id)) || links != f.links(id) {
			t.Fatal("failed owner move left parent or changed linked CRM")
		}
		f.assertLockFree()
	})
	t.Run("two SQL editors using one revision have one winner", func(t *testing.T) { testConcurrentVacancyEdit(t) })
}
func testConcurrentVacancyEdit(t *testing.T) {
	f := openVacancyCreateFixture(t)
	created := f.created(f.repo, f.input("cas"))
	id := created.ID
	before := f.detail("Demo", id)
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	var repos [2]vacancies.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err := db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
			t.Fatal("cannot identify CAS editor")
		}
		repos[i] = vacancies.Repository{DB: db}
	}
	if ids[0] == ids[1] {
		t.Fatal("CAS edit requires independent SQL connections")
	}
	holder, err := f.db.Conn(ctx)
	if err != nil {
		t.Fatal("cannot start edit barrier")
	}
	defer holder.Close()
	var locked int
	if err = holder.QueryRowContext(ctx, `SELECT GET_LOCK('itmap:manual-vacancy:create',0)`).Scan(&locked); err != nil || locked != 1 {
		t.Fatal("cannot hold edit barrier")
	}
	release := func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		holder.ExecContext(c, `DO RELEASE_LOCK('itmap:manual-vacancy:create')`)
	}
	var wg sync.WaitGroup
	var outcomes [2]error
	defer func() { cancel(); release(); wg.Wait() }()
	for i := range repos {
		input := f.editInput(before)
		if i == 0 {
			input.Title = "CI CAS editor A"
		} else {
			input.Title = "CI CAS editor B"
		}
		wg.Add(1)
		go func(i int, input vacancies.UpdateRequest) {
			defer wg.Done()
			_, outcomes[i] = repos[i].UpdateManual(ctx, "Demo", id, input)
		}(i, input)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE '%GET_LOCK(%manual-vacancy:create%'`, ids[0], ids[1]).Scan(&waiting); err != nil {
			t.Fatal("cannot observe CAS editors")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both editors never reached SQL barrier")
		case <-tick.C:
		}
	}
	release()
	wg.Wait()
	winner, conflict := 0, 0
	for _, err := range outcomes {
		if err == nil {
			winner++
		} else if errors.Is(err, vacancies.ErrVersionConflict) {
			conflict++
		} else {
			t.Fatalf("unexpected CAS outcome: %T", err)
		}
	}
	if winner != 1 || conflict != 1 || f.detail("Demo", id).Revision == before.Revision {
		t.Fatal("same revision must permit exactly one editor")
	}
	f.assertLockFree()
}
