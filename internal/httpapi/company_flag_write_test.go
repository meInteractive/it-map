package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/crm"
)

type companyFlagWriteProbe struct {
	calls               int
	user, company, flag string
	value               bool
	err                 error
}

func (p *companyFlagWriteProbe) SetCompanyFlag(_ context.Context, user, company, flag string, value bool) (crm.CompanyState, error) {
	p.calls++
	p.user = user
	p.company = company
	p.flag = flag
	p.value = value
	if p.err != nil {
		return crm.CompanyState{}, p.err
	}
	return crm.CompanyState{Favorite: flag == "favorite" && value, Hidden: flag == "hidden" && value, History: []crm.StatusChange{}}, nil
}
func TestCompanyFlagWriteRoutes(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("a", 64), map[string]string{"eugene": "admin", "demo": "password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := manager.Authenticate("demo", "password")
	token, _ := manager.Issue(u, time.Now())
	p := &companyFlagWriteProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CompanyFlagWrites: p})
	call := func(path, body, origin string, logged bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if logged {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	cases := []struct{ path, key string }{
		{"/api/crm/favorite?company=Example", "favorite"},
		{"/api/crm/hidden-company?company=Example", "hidden"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			body := `{"` + tc.key + `":true}`
			initial := p.calls
			if w := call(tc.path, body, "", false); w.Code != 401 || p.calls != initial {
				t.Fatal(w.Code)
			}
			if w := call(tc.path, body, "https://attacker.example", true); w.Code != 403 || p.calls != initial {
				t.Fatal(w.Code)
			}
			if w := call(tc.path, `{"favorite":true,"hidden":true}`, "", true); w.Code != 400 || p.calls != initial {
				t.Fatal(w.Code)
			}
			if w := call(tc.path, `{"`+tc.key+`":1}`, "", true); w.Code != 400 || p.calls != initial {
				t.Fatal(w.Code)
			}
			if w := call(tc.path, body, "http://attacker.example", true); w.Code != 403 || p.calls != initial {
				t.Fatal(w.Code)
			}
			n := p.calls
			w := call(tc.path, body, "", true)
			if w.Code != 200 || p.calls != n+1 || p.user != "Demo" || p.company != "Example" || p.flag != tc.key || !p.value {
				t.Fatal(w.Code, w.Body.String(), p)
			}
			if !strings.Contains(w.Body.String(), `"history":[]`) {
				t.Fatal(w.Body.String())
			}
		})
	}
	p.err = crm.ErrCompanyNotFound
	if w := call(cases[0].path, `{"favorite":true}`, "", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	p.err = crm.ErrAmbiguousState
	if w := call(cases[0].path, `{"favorite":true}`, "", true); w.Code != 409 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("PASSWORD=do-not-echo")
	if w := call(cases[0].path, `{"favorite":true}`, "", true); w.Code != 503 || strings.Contains(w.Body.String(), "do-not-echo") {
		t.Fatal(w.Code, w.Body.String())
	}
	// No auth and no explicit router entry: write APIs stay closed.
	h := NewHandlerWithOptions(nil, Options{Auth: manager})
	r := httptest.NewRequest("PUT", cases[0].path, strings.NewReader(`{"favorite":true}`))
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
