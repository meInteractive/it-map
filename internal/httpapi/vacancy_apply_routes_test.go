package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type vacancyApplyProbe struct {
	input     applications.VacancyApplyInput
	user      string
	vacancyID int64
	calls     int
	result    applications.VacancyApplyResult
	err       error
}

func (p *vacancyApplyProbe) ApplyToVacancy(_ context.Context, user string, id int64, input applications.VacancyApplyInput) (applications.VacancyApplyResult, error) {
	p.calls++
	p.input, p.user, p.vacancyID = input, user, id
	return p.result, p.err
}

func TestVacancyApplyHTTPContract(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"demo": "pass1", "eugene": "pass2"})
	if err != nil {
		t.Fatal(err)
	}
	usr, _ := manager.Authenticate("demo", "pass1")
	signed, _ := manager.Issue(usr, time.Now())
	p := &vacancyApplyProbe{result: applications.VacancyApplyResult{OK: true, CreatedApplicationID: 33, Vacancy: vacancies.Detail{}}}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyApplies: p})
	request := func(path, body, contentType, origin string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://itmap.test"+path, strings.NewReader(body))
		if cookie {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: signed})
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	valid := `{"companyName":"Example","idempotencyKey":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","creationContext":"ac1.token"}`
	for _, tc := range []struct {
		path, body, mime, origin string
		signed                   bool
		expected                 int
	}{
		{"/api/vacancies/12/apply/create", valid, "application/json", "", false, 401},
		{"/api/vacancies/12/apply/create", valid, "application/json", "https://evil.test", true, 403},
		{"/api/vacancies/12/apply/create", valid, "text/plain", "", true, 400},
		{"/api/vacancies/0/apply/create", valid, "application/json", "", true, 400},
		{"/api/vacancies/12/apply/create", `[]`, "application/json", "", true, 400},
		{"/api/vacancies/12/apply/create", `{`, "application/json", "", true, 400},
		{"/api/vacancies/12/apply/create", valid + ` {}`, "application/json", "", true, 400},
		{"/api/vacancies/12/apply/create", strings.Repeat(" ", 4096) + valid, "application/json", "", true, 400},
		{"/api/vacancies/12/apply/create", valid, "application/json", "https://itmap.test", true, 200},
		{"/api/vacancies/12/apply", valid, "application/json", "", true, 200},
	} {
		w := request(tc.path, tc.body, tc.mime, tc.origin, tc.signed)
		if w.Code != tc.expected {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if p.calls != 2 || p.user != "Demo" || p.vacancyID != 12 || p.input.CompanyName != "Example" {
		t.Fatalf("got %+v", p)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{applications.ErrCreationContextRequired, 428, "creation_context_required"},
		{applications.ErrCreationContextExpired, 409, "creation_context_expired"},
		{applications.ErrIntentConflict, 409, "intent_conflict"},
		{applications.ErrVacancyOwnerChanged, 409, "vacancy_owner_changed"},
		{applications.ErrAmbiguousVacancyLink, 409, ""},
		{applications.ErrApplicationDeleted, 410, "application_deleted"},
		{applications.ErrShortcutVacancyMissing, 404, ""},
		{applications.ErrInvalidInput, 400, ""},
		{errors.New("DB_SECRET=do-not-leak"), 503, ""},
	} {
		p.err = tc.err
		w := request("/api/vacancies/12/apply/create", valid, "application/json", "", true)
		if w.Code != tc.status || (tc.code != "" && !strings.Contains(w.Body.String(), tc.code)) || strings.Contains(w.Body.String(), "do-not-leak") {
			t.Fatalf("%v: %d %s", tc.err, w.Code, w.Body.String())
		}
	}
}
