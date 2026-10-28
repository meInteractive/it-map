//go:build mariadb && vacancycreates && vacancyedits && vacancydeletes && v39restore && restorehttp && applicationlinks

package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const appLinkKey = "00000000-0000-4000-9000-000000000203"

type appLinkOutcome struct {
	created applications.CreationResult
	err     error
	status  int
}

func appLinkBarrier(t *testing.T, prefix string) (*mutationFault, *sql.DB, func()) {
	t.Helper()
	fault, db := openMutationFaultPool(t, prefix)
	fault.entered, fault.release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(fault.release) }) }
	t.Cleanup(release)
	return fault, db, release
}
func appLinkEntered(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("application link SQL barrier not reached")
	}
}
func appLinkDone(t *testing.T, ch <-chan appLinkOutcome) appLinkOutcome {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(8 * time.Second):
		t.Fatal("application link actor did not finish")
		return appLinkOutcome{}
	}
}
func appLinkWait(t *testing.T, ctx context.Context, db *sql.DB, prefixes ...string) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	args := []any{}
	clauses := []string{}
	for _, p := range prefixes {
		args = append(args, p+"%")
		clauses = append(clauses, "INFO LIKE ?")
	}
	q := "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND (" + strings.Join(clauses, " OR ") + ")"
	for {
		var n int
		if e := db.QueryRowContext(ctx, q, args...).Scan(&n); e != nil {
			t.Fatal("cannot observe application link waiter")
		}
		if n > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("application link actor did not reach actual SQL wait")
		case <-ctx.Done():
			t.Fatal("application link fixture timed out")
		case <-tick.C:
		}
	}
}
func openAppLinkFixture(t *testing.T) (*vacancyCreateFixture, applications.Repository, int64, applications.CreateInput) {
	t.Helper()
	f := openVacancyCreateFixture(t)
	if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", appLinkKey) != 0 {
		t.Fatal("application link intent namespace occupied")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := f.db.ExecContext(ctx, "DELETE FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", appLinkKey); e != nil {
			t.Error("cannot clean synthetic application link receipt")
		}
	})
	v := f.created(f.repo, f.input("application-links"))
	other := f.insert("INSERT INTO company_applications(user_name,company_name,vacancy_id,status,comment_md,application_sources)VALUES('Nastya',?,?,'Откликнулся','CI Nastya private','[\"site\"]')", crudCompany, v.ID)
	f.exec("INSERT INTO company_application_status_history(application_id,from_status,to_status)VALUES(?,'','Откликнулся')", other)
	f.exec("INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at)VALUES('Nastya',?,?,'call','CI retained task','2026-10-26T12:00:00Z')", crudCompany, v.ID)
	repo := applications.Repository{DB: f.db, CreationSecret: []byte(strings.Repeat("l", 64))}
	token, e := repo.CreationContext(f.ctx, "Demo")
	if e != nil {
		t.Fatal("cannot issue synthetic application link context")
	}
	in := applications.CreateInput{CompanyName: crudCompany, VacancyID: &v.ID, Status: "Откликнулся", Comment: "CI Demo link", ApplicationSources: []string{"site"}, IdempotencyKey: appLinkKey, CreationContext: token}
	return f, repo, v.ID, in
}
func appLinkCreate(repo applications.Repository, ctx context.Context, in applications.CreateInput) appLinkOutcome {
	out, e := repo.CreateWithReceipt(ctx, "Demo", in)
	status := 201
	if e != nil {
		status = 500
		if errors.Is(e, applications.ErrInvalidVacancy) {
			status = 400
		}
	}
	return appLinkOutcome{created: out, err: e, status: status}
}

// Live authenticated HTTP uses the same synthetic creation secret and real SQL
// repositories. Nastya's private comment must never enter Demo's response.
func appLinkHTTP(t *testing.T, f *vacancyCreateFixture, ar applications.Repository, vr vacancies.Repository) func(string, string, string, any) appLinkOutcome {
	t.Helper()
	m, e := auth.NewManager(strings.Repeat("l", 64), map[string]string{"eugene": "CI_LINK_PASSWORD", "demo": "CI_LINK_PASSWORD"})
	if e != nil {
		t.Fatal("cannot initialize link auth")
	}
	server := httptest.NewTLSServer(httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, Applications: ar, ApplicationCompanies: ar, ApplicationCreates: ar, ApplicationWrites: ar, Vacancies: vr, VacancyDetails: vr, VacancyUpdates: vr, VacancyDeletes: vr}))
	t.Cleanup(server.Close)
	cookies := map[string]*http.Cookie{}
	for _, login := range []string{"demo", "eugene"} {
		u, ok := m.Authenticate(login, "CI_LINK_PASSWORD")
		if !ok {
			t.Fatal("synthetic link login unavailable")
		}
		token, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal("synthetic link session unavailable")
		}
		cookies[login] = &http.Cookie{Name: auth.CookieName, Value: token}
	}
	return func(user, method, path string, payload any) appLinkOutcome {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req, e := http.NewRequestWithContext(f.ctx, method, server.URL+path, bytes.NewReader(body))
		if e != nil {
			return appLinkOutcome{err: e}
		}
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookies[user])
		resp, e := server.Client().Do(req)
		if e != nil {
			return appLinkOutcome{err: e}
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(resp.Body)
		if e != nil {
			return appLinkOutcome{err: e}
		}
		if resp.Header.Get("Cache-Control") != "private, no-store" || bytes.Contains(data, []byte("CI Nastya private")) {
			return appLinkOutcome{err: errors.New("application link response cache/isolation boundary failed")}
		}
		out := appLinkOutcome{status: resp.StatusCode}
		if strings.Contains(path, "/api/applications/create") && resp.StatusCode < 300 {
			if e = json.Unmarshal(data, &out.created); e != nil {
				return appLinkOutcome{err: e}
			}
		}
		return out
	}
}
func runAppLinkMutation(t *testing.T, removeVacancy, firstCreate, httpMode bool) {
	t.Helper()
	f, repo, id, input := openAppLinkFixture(t)
	if removeVacancy {
		if out, e := f.repo.DeleteManual(f.ctx, id); e != nil || !out.Archived {
			t.Fatal("linked vacancy must archive before hard deletion")
		}
	}
	edit := f.editInput(f.detail("Eugene", id))
	edit.CompanyName = crudMoveCompany
	prefix := "SELECT id FROM company_vacancies WHERE id="
	if !firstCreate {
		prefix = "UPDATE company_vacancies SET company_name="
		if removeVacancy {
			prefix = "DELETE FROM company_vacancies"
		}
	}
	fault, db, release := appLinkBarrier(t, prefix)
	ar, vr := repo, f.repo
	ar.DB, vr.DB = db, db
	create := func() appLinkOutcome { return appLinkCreate(ar, f.ctx, input) }
	mutate := func() appLinkOutcome {
		var e error
		if removeVacancy {
			_, e = vr.DeleteManual(f.ctx, id)
		} else {
			_, e = vr.UpdateManual(f.ctx, "Eugene", id, edit)
		}
		status := 200
		if e != nil {
			status = 500
		}
		return appLinkOutcome{err: e, status: status}
	}
	if httpMode {
		call := appLinkHTTP(t, f, ar, vr)
		create = func() appLinkOutcome { return call("demo", "POST", "/api/applications/create", input) }
		edit.CoverLetterScope = "shared"
		mutate = func() appLinkOutcome {
			if removeVacancy {
				return call("eugene", "DELETE", fmt.Sprintf("/api/vacancies/%d", id), nil)
			}
			return call("eugene", "PUT", fmt.Sprintf("/api/vacancies/%d", id), edit)
		}
	}
	created, mutated := make(chan appLinkOutcome, 1), make(chan appLinkOutcome, 1)
	if firstCreate {
		go func() { created <- create() }()
	} else {
		go func() { mutated <- mutate() }()
	}
	appLinkEntered(t, fault.entered)
	if firstCreate {
		go func() { mutated <- mutate() }()
		appLinkWait(t, f.ctx, f.db, "SELECT id FROM company_vacancies", "SELECT company_name, entity_type, archived FROM company_vacancies")
	} else {
		go func() { created <- create() }()
		appLinkWait(t, f.ctx, f.db, "SELECT id FROM company_vacancies", "INSERT IGNORE INTO company_crm", "SELECT company_name FROM company_crm")
	}
	release()
	c, m := appLinkDone(t, created), appLinkDone(t, mutated)
	if m.status != 200 {
		t.Fatalf("vacancy link mutation failed: %d (%T)", m.status, m.err)
	}
	expectedCreate := 400
	if firstCreate {
		expectedCreate = 201
	}
	if c.status != expectedCreate {
		t.Fatalf("application/vacancy ordering differs: status%d expected%d (%T)", c.status, expectedCreate, c.err)
	}
	owner := crudMoveCompany
	if removeVacancy {
		owner = crudCompany
		if f.count("SELECT COUNT(*) FROM company_vacancies WHERE id=?", id) != 0 {
			t.Fatal("hard delete left vacancy")
		}
	} else if f.count("SELECT COUNT(*) FROM company_vacancies WHERE id=? AND company_name=?", id, owner) != 1 {
		t.Fatal("owner move lost vacancy")
	}
	apps := 1
	if firstCreate {
		apps = 2
	}
	if f.count("SELECT COUNT(*) FROM company_applications WHERE company_name=?", owner) != apps || f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name='Nastya' AND company_name=?", owner) != 1 || f.count("SELECT COUNT(*) FROM company_applications WHERE user_name='Nastya' AND company_name=? AND comment_md='CI Nastya private'", owner) != 1 {
		t.Fatal("cross-module mutation lost another user's CRM")
	}
	links := apps
	if removeVacancy {
		links = 0
	}
	if f.count("SELECT COUNT(*) FROM company_applications WHERE company_name=? AND vacancy_id=?", owner, id) != links {
		t.Fatal("cross-module mutation left incorrect vacancy FK")
	}
	expectedReceipts := 0
	if firstCreate {
		expectedReceipts = 1
	}
	if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", appLinkKey) != expectedReceipts {
		t.Fatal("failed creation left receipt or successful intent lost receipt")
	}
	if f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.company_name=?", owner) != apps {
		t.Fatal("link mutation lost or duplicated status history")
	}
	if firstCreate {
		replay, e := repo.CreateWithReceipt(f.ctx, "Demo", input)
		if e != nil || !replay.Replayed || replay.CreatedApplicationID != c.created.CreatedApplicationID {
			t.Fatal("owner/detach mutation lost durable replay identity")
		}
		if f.count("SELECT COUNT(*) FROM company_applications WHERE company_name=?", owner) != apps {
			t.Fatal("replay recreated detached or moved application")
		}
	}
}
