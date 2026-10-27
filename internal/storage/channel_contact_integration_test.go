//go:build mariadb && channelwrites && contactwrites && channelcontacts

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/backup"
	"itmap/internal/channels"
	"itmap/internal/contacts"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
	"net/url"
	"reflect"
	"testing"
	"time"
)

const jointCompany = "CI Channel Employer"

func openJointFixture(t *testing.T) (*channelFixture, contacts.Repository, int64) {
	t.Helper()
	f := openChannelFixture(t)
	for _, table := range []string{"hiring_contacts", "vacancy_roles", "vacancy_role_assignments", "company_vacancy_user_state"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("joint fixture requires empty synthetic children")
		}
	}
	role := f.exec("INSERT INTO vacancy_roles(stable_key,name)VALUES('ci-go075-leaf','CI Joint Role')")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []string{"DELETE FROM hiring_contacts", "DELETE FROM company_vacancies", "DELETE FROM vacancy_roles WHERE stable_key='ci-go075-leaf'"} {
			if _, e := f.db.ExecContext(ctx, q); e != nil {
				t.Error("cannot clean joint synthetic rows")
			}
		}
		for _, table := range []string{"hiring_contacts", "vacancy_roles", "vacancy_role_assignments", "company_vacancy_user_state"} {
			var n int
			if e := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 0 {
				t.Error("joint gate leaked child rows")
			}
		}
	})
	return f, contacts.Repository{DB: f.db, CatalogNames: backup.StoredCatalogNames}, role
}
func jointVacancy(t *testing.T, f *channelFixture, c, role int64) int64 {
	t.Helper()
	f.exec("INSERT INTO company_crm(company_name)VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)", jointCompany)
	v := f.exec("INSERT INTO company_vacancies(company_name,entity_type,url,title,role,level,description_md)VALUES(?,'company','https://example.invalid/go075','CI Joint Vacancy','CI Joint Role','Middle','CI Joint Description')", jointCompany)
	f.exec("INSERT INTO vacancy_role_assignments(vacancy_id,role_id)VALUES(?,?)", v, role)
	f.exec("INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url)VALUES(?,?,?)", v, c, "https://t.me/ci_joint_a/1")
	return v
}
func jointRequest(t *testing.T, f *channelFixture, v, c, role int64) vacancies.UpdateRequest {
	t.Helper()
	d, e := (vacancies.Repository{DB: f.db}).DetailByID(f.ctx, "Eugene", v)
	if e != nil {
		t.Fatal("cannot read joint vacancy revision")
	}
	return vacancies.UpdateRequest{Revision: d.Vacancy.Revision, TelegramProvided: true, Input: vacancies.Input{CompanyName: jointCompany, EntityType: "channel", URL: "https://example.invalid/go075", Title: "CI Joint Vacancy", Level: "Middle", DescriptionMD: "CI Joint Description", RoleIDs: []int64{role}, TelegramChannelID: &c, TelegramMessageURL: "https://t.me/ci_joint_a/1"}}
}
func TestMariaDBChannelContactDisposable(t *testing.T) {
	t.Run("shared authenticated API preserves CRM contacts and sources", func(t *testing.T) {
		f, contactRepo, role := openJointFixture(t)
		vr := vacancies.Repository{DB: f.db}
		h := openChannelHTTP(t, f, f.repo, func(o *httpapi.Options) { o.Contacts = contactRepo; o.VacancyUpdates = vr; o.VacancyDetails = vr })
		call := func(method, path string, payload any, want int) []byte {
			t.Helper()
			var body []byte
			if payload != nil {
				body, _ = json.Marshal(payload)
			}
			status, data := h.call("eugene", method, path, h.server.URL, body)
			if status != want {
				t.Fatalf("joint %s %s status %d expected %d", method, path, status, want)
			}
			return data
		}
		for _, p := range []int{1, 2, 3} {
			call("POST", "/api/hiring-contacts", map[string]any{"companyName": jointCompany, "position": p, "fullName": fmt.Sprintf("CI Contact %d", p)}, 201)
		}
		before, e := contactRepo.List(f.ctx, jointCompany)
		if e != nil {
			t.Fatal("joint contacts unavailable")
		}
		var created channels.CreateResult
		if json.Unmarshal(call("POST", "/api/channels", map[string]any{"name": "CI Joint A", "username": "@ci_joint_a"}, 201), &created) != nil {
			t.Fatal("invalid joint channel reply")
		}
		a := created.ChannelID
		v := jointVacancy(t, f, a, role)
		req := jointRequest(t, f, v, a, role)
		req.CoverLetterScope = "shared"
		call("PUT", fmt.Sprintf("/api/vacancies/%d", v), req, 200)
		call("DELETE", fmt.Sprintf("/api/channels/%d", a), nil, 409)
		call("PUT", fmt.Sprintf("/api/hiring-contacts/%d", before[0].ID), map[string]any{"fullName": "CI Contact Updated"}, 200)
		call("DELETE", fmt.Sprintf("/api/hiring-contacts/%d", before[1].ID), nil, 200)
		if f.count("SELECT COUNT(*) FROM company_crm WHERE company_name=?", jointCompany) != 1 || f.count("SELECT COUNT(*) FROM company_vacancies WHERE id=?", v) != 1 || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?", v) != 1 {
			t.Fatal("contact edits deleted CRM/vacancy/source")
		}
		// A second source permits detach while preserving the vacancy and contacts.
		var alternative channels.CreateResult
		_ = json.Unmarshal(call("POST", "/api/channels", map[string]any{"name": "CI Joint B", "username": "@ci_joint_b"}, 201), &alternative)
		f.link(v, alternative.ChannelID)
		call("DELETE", fmt.Sprintf("/api/channels/%d", a), nil, 200)
		call("DELETE", fmt.Sprintf("/api/channels/%d", alternative.ChannelID), nil, 409)
		remaining, e := contactRepo.List(f.ctx, jointCompany)
		if e != nil || len(remaining) != 2 || remaining[0].FullName != "CI Contact Updated" || remaining[1].ID != before[2].ID {
			t.Fatal("channel deletes lost retained contacts")
		}
		for _, user := range []string{"demo", "nastya"} {
			status, data := h.call(user, "GET", "/api/hiring-contacts?company="+url.QueryEscape(jointCompany), "", nil)
			var got struct{ Contacts []contacts.Contact }
			if status != 200 || json.Unmarshal(data, &got) != nil || !reflect.DeepEqual(got.Contacts, remaining) {
				t.Fatal("shared contacts differ between authenticated readers")
			}
		}
		// Contact ID reuse/slot reuse must not touch channel/vacancy provenance.
		call("POST", "/api/hiring-contacts", map[string]any{"companyName": jointCompany, "position": 2, "fullName": "CI Replacement"}, 201)
		if f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=? AND channel_id=?", v, alternative.ChannelID) != 1 {
			t.Fatal("contact slot reuse changed source links")
		}
	})
	t.Run("actual vacancy editor wins then source delete returns conflict", func(t *testing.T) {
		f, cr, role := openJointFixture(t)
		if _, e := cr.Create(f.ctx, jointCompany, contactRaw(1, "CI retained")); e != nil {
			t.Fatal("cannot seed joint contact")
		}
		a := f.create("CI Joint A", "@ci_joint_a")
		v := jointVacancy(t, f, a, role)
		req := jointRequest(t, f, v, a, role)
		before, _ := cr.List(f.ctx, jointCompany)
		fault, db := openMutationFaultPool(t, "UPDATE company_vacancies SET company_name=")
		fault.entered, fault.release = make(chan struct{}), make(chan struct{})
		released := false
		release := func() {
			if !released {
				close(fault.release)
				released = true
			}
		}
		t.Cleanup(release)
		one, two := make(chan error, 1), make(chan error, 1)
		go func() { _, e := (vacancies.Repository{DB: db}).UpdateManual(f.ctx, "Eugene", v, req); one <- e }()
		awaitChannelEvent(t, fault.entered)
		go func() { _, e := f.repo.Delete(f.ctx, a); two <- e }()
		waitChannelSQL(t, f, "SELECT id FROM company_vacancies WHERE id=")
		release()
		if e := awaitChannelError(t, one); e != nil {
			t.Fatal("actual vacancy edit failed")
		}
		if !errors.Is(awaitChannelError(t, two), channels.ErrSoleSource) {
			t.Fatal("delete ignored committed actual editor owner")
		}
		after, _ := cr.List(f.ctx, jointCompany)
		if !reflect.DeepEqual(before, after) || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?", v) != 1 {
			t.Fatal("editor/delete race lost contacts or last source")
		}
	})
	t.Run("source delete wins then stale vacancy editor conflicts", func(t *testing.T) {
		f, cr, role := openJointFixture(t)
		if _, e := cr.Create(f.ctx, jointCompany, contactRaw(1, "CI retained")); e != nil {
			t.Fatal("cannot seed joint contact")
		}
		a := f.create("CI Joint A", "@ci_joint_a")
		v := jointVacancy(t, f, a, role)
		req := jointRequest(t, f, v, a, role)
		before, _ := cr.List(f.ctx, jointCompany)
		repo, entered, release := channelPause(t, f, "DELETE FROM telegram_channels")
		one, two := make(chan error, 1), make(chan error, 1)
		go func() { _, e := repo.Delete(f.ctx, a); one <- e }()
		awaitChannelEvent(t, entered)
		go func() { _, e := (vacancies.Repository{DB: f.db}).UpdateManual(f.ctx, "Eugene", v, req); two <- e }()
		waitChannelSQL(t, f, "SELECT id FROM company_vacancies WHERE id=")
		release()
		if awaitChannelError(t, one) != nil || !errors.Is(awaitChannelError(t, two), vacancies.ErrVersionConflict) {
			t.Fatal("stale editor did not conflict after source detach")
		}
		after, _ := cr.List(f.ctx, jointCompany)
		if !reflect.DeepEqual(before, after) || f.count("SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?", v) != 0 || f.count("SELECT COUNT(*) FROM company_vacancies WHERE id=? AND entity_type='company'", v) != 1 {
			t.Fatal("stale editor changed owner/contacts or resurrected deleted source")
		}
	})
}
