//go:build mariadb && vacancycreates && vacancyedits && appvacancyraces

package storage

import (
	"bytes"
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

type appVacancyOutcome struct {
	status int
	err    error
}

func awaitAppVacancy(t *testing.T, ch <-chan appVacancyOutcome) appVacancyOutcome {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(8 * time.Second):
		t.Fatal("application/vacancy writer did not finish")
		return appVacancyOutcome{}
	}
}
func waitAppVacancySQL(t *testing.T, f *vacancyCreateFixture, prefix string) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND INFO LIKE ?", prefix+"%") > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("actual cross-module SQL lock was not reached")
		case <-f.ctx.Done():
			t.Fatal("cross-module fixture timed out")
		case <-tick.C:
		}
	}
}
func runAppVacancyRace(t *testing.T, httpMode, applicationFirst bool) {
	t.Helper()
	f := openVacancyCreateFixture(t)
	v := f.created(f.repo, f.input("app-vacancy-race"))
	id := v.ID
	f.linked(id)
	originalRepo := applications.Repository{DB: f.db}
	rows, e := originalRepo.List(f.ctx, "Demo", crudCompany, nil)
	if e != nil || len(rows) != 1 {
		t.Fatal("cannot read linked synthetic application")
	}
	app := rows[0]
	input := applications.UpdateInput{Revision: app.Revision, VacancyID: &id, Status: "Получил ответ", Comment: "CI cross-module update", ApplicationSources: []string{"site"}}
	edit := f.editInput(f.detail("Eugene", id))
	edit.CompanyName = crudMoveCompany
	prefix := "SELECT a.company_name,a.status,"
	if !applicationFirst {
		prefix = "UPDATE company_vacancies SET company_name="
	}
	fault, db := openMutationFaultPool(t, prefix)
	fault.entered, fault.release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(fault.release) }) }
	t.Cleanup(release)
	ar, vr := applications.Repository{DB: db}, (vacancies.Repository{DB: db})
	one, two := make(chan appVacancyOutcome, 1), make(chan appVacancyOutcome, 1)
	appCall := func() appVacancyOutcome {
		_, e := ar.Update(f.ctx, "Demo", app.ID, input)
		status := 200
		if errors.Is(e, applications.ErrConflict) {
			status = 409
		} else if e != nil {
			status = 500
		}
		return appVacancyOutcome{status: status, err: e}
	}
	vacancyCall := func() appVacancyOutcome {
		_, e := vr.UpdateManual(f.ctx, "Eugene", id, edit)
		status := 200
		if errors.Is(e, vacancies.ErrVersionConflict) {
			status = 409
		} else if e != nil {
			status = 500
		}
		return appVacancyOutcome{status: status, err: e}
	}
	if httpMode {
		manager, e := auth.NewManager(strings.Repeat("r", 64), map[string]string{"demo": "CI_RACE_PASSWORD", "eugene": "CI_RACE_PASSWORD"})
		if e != nil {
			t.Fatal("cannot initialize synthetic race auth")
		}
		h := httptest.NewTLSServer(httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, Applications: ar, ApplicationCompanies: ar, ApplicationWrites: ar, Vacancies: vr, VacancyDetails: vr, VacancyUpdates: vr}))
		t.Cleanup(h.Close)
		cookies := map[string]*http.Cookie{}
		for _, login := range []string{"demo", "eugene"} {
			user, ok := manager.Authenticate(login, "CI_RACE_PASSWORD")
			if !ok {
				t.Fatal("synthetic race account unavailable")
			}
			token, e := manager.Issue(user, time.Now())
			if e != nil {
				t.Fatal("synthetic race token unavailable")
			}
			cookies[login] = &http.Cookie{Name: auth.CookieName, Value: token}
		}
		call := func(user, path string, payload any) appVacancyOutcome {
			body, e := json.Marshal(payload)
			if e != nil {
				return appVacancyOutcome{err: e}
			}
			req, e := http.NewRequestWithContext(f.ctx, "PUT", h.URL+path, bytes.NewReader(body))
			if e != nil {
				return appVacancyOutcome{err: e}
			}
			req.Header.Set("Origin", h.URL)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookies[user])
			resp, e := h.Client().Do(req)
			if e != nil {
				return appVacancyOutcome{err: e}
			}
			defer resp.Body.Close()
			data, e := io.ReadAll(resp.Body)
			if e != nil {
				return appVacancyOutcome{err: e}
			}
			if resp.Header.Get("Cache-Control") != "private, no-store" {
				return appVacancyOutcome{err: errors.New("race reply is not private")}
			}
			if resp.StatusCode == 409 {
				var reply struct{ Error string }
				if json.Unmarshal(data, &reply) != nil || reply.Error == "" || strings.Contains(string(data), "1213") {
					return appVacancyOutcome{err: errors.New("race conflict exposed SQL or invalid JSON")}
				}
			}
			return appVacancyOutcome{status: resp.StatusCode}
		}
		appCall = func() appVacancyOutcome { return call("demo", fmt.Sprintf("/api/applications/%d", app.ID), input) }
		// The HTTP form explicitly includes both empty/shared letter properties.
		edit.CoverLetterScope = "shared"
		vacancyCall = func() appVacancyOutcome { return call("eugene", fmt.Sprintf("/api/vacancies/%d", id), edit) }
	}
	if applicationFirst {
		go func() { one <- appCall() }()
	} else {
		go func() { two <- vacancyCall() }()
	}
	select {
	case <-fault.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first real SQL barrier not reached")
	}
	if applicationFirst {
		go func() { two <- vacancyCall() }()
		waitAppVacancySQL(t, f, "UPDATE company_applications SET company_name=")
	} else {
		go func() { one <- appCall() }()
		waitAppVacancySQL(t, f, "SELECT id FROM company_vacancies")
	}
	release()
	appResult, vacancyResult := awaitAppVacancy(t, one), awaitAppVacancy(t, two)
	for _, result := range []appVacancyOutcome{appResult, vacancyResult} {
		if result.status != 200 && result.status != 409 {
			t.Fatalf("cross-module deadlock was not recovered to success/CAS: %d (%T)", result.status, result.err)
		}
	}
	if appResult.status == 409 && vacancyResult.status == 409 {
		t.Fatal("cross-module retry lost both writers")
	}
	fault.mu.Lock()
	victims := fault.deadlocks
	fault.mu.Unlock()
	if victims < 1 {
		t.Fatal("barriers did not cause an actual InnoDB deadlock")
	}
	current, e := originalRepo.ByID(f.ctx, "Demo", app.ID)
	if e != nil {
		t.Fatal("cross-module race deleted application")
	}
	detail := f.detail("Eugene", id)
	if current.CompanyName != detail.CompanyName || current.VacancyID == nil || *current.VacancyID != id {
		t.Fatal("cross-module retry left mismatched application owner/FK")
	}
	if vacancyResult.status == 200 && detail.CompanyName != crudMoveCompany {
		t.Fatal("successful vacancy move was lost")
	}
	if vacancyResult.status == 409 && detail.CompanyName != crudCompany {
		t.Fatal("conflicting vacancy editor left partial owner")
	}
	status, comment, history := "Откликнулся", "CI linked comment", 2
	if appResult.status == 200 {
		status, comment, history = "Получил ответ", "CI cross-module update", 3
	}
	if current.Status != status || current.Comment != comment || f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.vacancy_id=?", id) != history {
		t.Fatal("deadlock rollback/retry lost or duplicated application history")
	}
	if f.count("SELECT COUNT(*) FROM company_applications WHERE vacancy_id=? AND company_name=?", id, detail.CompanyName) != 2 || f.count("SELECT COUNT(*) FROM company_tasks WHERE vacancy_id=? AND company_name=?", id, detail.CompanyName) != 2 || f.count("SELECT COUNT(*) FROM company_applications WHERE vacancy_id=? AND user_name='Nastya' AND status='Откликнулся' AND comment_md='CI linked comment'", id) != 1 {
		t.Fatal("cross-module recovery lost another owner/task link")
	}
	// A fresh CAS retry remains usable and cannot append duplicate history.
	input.Revision = current.Revision
	input.Status = current.Status
	input.Comment = current.Comment
	if _, e = originalRepo.Update(f.ctx, "Demo", app.ID, input); e != nil {
		t.Fatal("fresh CAS retry failed after deadlock recovery")
	}
	if f.count("SELECT COUNT(*) FROM company_application_status_history h JOIN company_applications a ON a.id=h.application_id WHERE a.vacancy_id=?", id) != history {
		t.Fatal("healthy retry duplicated status history")
	}
}
func TestMariaDBApplicationVacancyRaceDisposable(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(map[bool]string{true: "application first", false: "vacancy first"}[first], func(t *testing.T) { runAppVacancyRace(t, false, first) })
	}
}
func TestMariaDBApplicationVacancyHTTPDisposable(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(map[bool]string{true: "application first", false: "vacancy first"}[first], func(t *testing.T) { runAppVacancyRace(t, true, first) })
	}
}
