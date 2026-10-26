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
	"itmap/internal/vacancies"
)

type companySaveProbe struct {
	calls         int
	user, company string
	input         crm.SaveInput
	err           error
}

func (p *companySaveProbe) SaveCompany(_ context.Context, user, company string, input crm.SaveInput) (crm.SaveResult, error) {
	p.calls++
	p.user = user
	p.company = company
	p.input = input
	return crm.SaveResult{CompanyState: crm.CompanyState{Status: input.Status, Comment: input.Comment, History: []crm.StatusChange{}}, Vacancies: []vacancies.CompanyVacancy{}}, p.err
}
func TestCompanySaveHTTPContract(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_SYNTHETIC_ADMIN_PASSWORD", "demo": "CI_SYNTHETIC_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "CI_SYNTHETIC_PASSWORD")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	probe := &companySaveProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CompanyCRMWrites: probe})
	call := func(body, origin, media string, logged bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest("PUT", "https://itmap.test/api/crm?company=Employer&userName=Nastya", strings.NewReader(body))
		if logged {
			request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		request.Header.Set("Content-Type", media)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if w := call(`{}`, "", "application/json", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, c := range []struct {
		body, origin, media string
		status              int
	}{
		{`{}`, "https://cross-site.invalid", "application/json", 403}, {`{}`, "null", "application/json", 403}, {`{}`, "", "text/plain", 400},
		{`{"status":"CI unknown"}`, "", "application/json", 400}, {`{"comment":true}`, "", "application/json", 400},
		{`{"comment":"a","comment":"b"}`, "", "application/json", 400}, {`{"userName":"Nastya"}`, "", "application/json", 400}, {`{"revision":"invented"}`, "", "application/json", 400}, {`{} {}`, "", "application/json", 400}, {`[]`, "", "application/json", 400},
		{`{"comment":"` + strings.Repeat("😀", 501) + `"}`, "", "application/json", 400},
	} {
		if w := call(c.body, c.origin, c.media, true); w.Code != c.status || probe.calls != 0 {
			t.Fatalf("guard status %d, expected %d", w.Code, c.status)
		}
	}
	if w := call(`{"status":"Откликнулся","comment":"  CI comment  "}`, "https://itmap.test", "application/json", true); w.Code != 200 || probe.user != "Demo" || probe.company != "Employer" || probe.input.Comment != "  CI comment  " || !strings.Contains(w.Body.String(), `"vacancies":[]`) || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("CRM PUT whole-form/owner/response contract failed")
	}
	for _, c := range []struct {
		err    error
		status int
	}{{crm.ErrCompanyNotFound, 404}, {crm.ErrAmbiguousState, 409}, {crm.ErrInvalidCompanyInput, 400}, {errors.New("CI_PRIVATE_SQL_DETAILS"), 503}} {
		probe.err = c.err
		if w := call(`{}`, "", "application/json", true); w.Code != c.status || strings.Contains(w.Body.String(), "CI_PRIVATE_SQL_DETAILS") {
			t.Fatal("CRM error mapping/disclosure failed")
		}
	}
}
