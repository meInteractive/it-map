package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/contacts"
	"net/http"
	"strings"
	"testing"
)

type contactProbe struct {
	calls int
	input contacts.Input
	id    int64
	err   error
}

func (p *contactProbe) List(context.Context, string) ([]contacts.Contact, error) {
	p.calls++
	return []contacts.Contact{}, p.err
}
func (p *contactProbe) Create(_ context.Context, _ string, in contacts.Input) (contacts.Result, error) {
	p.calls++
	p.input = in
	return contacts.Result{OK: true, Contacts: []contacts.Contact{}}, p.err
}
func (p *contactProbe) Update(_ context.Context, id int64, in contacts.Input) (contacts.Result, error) {
	p.calls++
	p.id = id
	p.input = in
	return contacts.Result{OK: true, Contacts: []contacts.Contact{}}, p.err
}
func (p *contactProbe) Delete(_ context.Context, id int64) (contacts.Result, error) {
	p.calls++
	p.id = id
	return contacts.Result{OK: true, Contacts: []contacts.Contact{}}, p.err
}
func TestContactAuthMethodsAndBodies(t *testing.T) {
	_, m, _ := channelWriteHTTP(t)
	p := &contactProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, Contacts: p})
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		path := "/api/hiring-contacts"
		if method != "POST" {
			path += "/7"
		}
		for _, v := range []struct {
			user, origin string
			status       int
		}{{"", "", 401}, {"demo", "", 403}, {"eugene", "https://other.invalid", 403}} {
			w := channelRequest(t, h, m, v.user, method, path, v.origin, `{}`)
			if w.Code != v.status || p.calls != 0 {
				t.Fatal("contact auth/origin boundary failed")
			}
		}
	}
	for _, v := range []struct {
		method, path, user string
		status             int
	}{{"PATCH", "/api/hiring-contacts", "demo", 405}, {"PATCH", "/api/hiring-contacts/7", "demo", 403}, {"PATCH", "/api/hiring-contacts/7", "eugene", 405}} {
		w := channelRequest(t, h, m, v.user, v.method, v.path, "", `{}`)
		if w.Code != v.status || p.calls != 0 {
			t.Fatal("source contact method/admin precedence changed")
		}
	}
	w := channelRequest(t, h, m, "eugene", "POST", "/api/hiring-contacts", "", `{"companyName":" CI Employer ","fullName":"old","fullName":"CI New","position":"2","unknown":true}`)
	if w.Code != 201 || string(p.input["fullName"]) != `"CI New"` || string(p.input["position"]) != `"2"` {
		t.Fatal("contact raw fields/duplicates changed")
	}
	before := p.calls
	for _, body := range []string{`{`, `{} {}`, `[]`} {
		w = channelRequest(t, h, m, "eugene", "PUT", "/api/hiring-contacts/7", "", body)
		if w.Code != 400 || p.calls != before {
			t.Fatal("malformed contact JSON reached repository")
		}
	}
	w = channelRequest(t, h, m, "eugene", "DELETE", "/api/hiring-contacts/0", "", `not JSON`)
	if w.Code != 200 || p.id != 0 {
		t.Fatal("contact delete body or zero ID changed")
	}
	p.err = errors.New("CI_PRIVATE_SQL_DETAILS")
	w = channelRequest(t, h, m, "eugene", "PUT", "/api/hiring-contacts/7", "", `{}`)
	var errReply struct{ Error string }
	if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &errReply) != nil || errReply.Error != "Не удалось обновить контакт." || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("contact SQL error leaked")
	}
	p.err = contacts.ErrCompanyNotFound
	w = channelRequest(t, h, m, "demo", "GET", "/api/hiring-contacts?company=unknown", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatal("unknown company not found changed")
	}
}
