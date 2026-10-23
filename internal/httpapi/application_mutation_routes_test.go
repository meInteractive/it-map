package httpapi

import (
	"context"
	"encoding/json"
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

type applicationMutatorProbe struct {
	calls        int
	method, user string
	id           int64
	input        applications.UpdateInput
	fail         error
}

func (p *applicationMutatorProbe) Update(_ context.Context, user string, id int64, in applications.UpdateInput) (string, error) {
	p.calls++
	p.method = "PUT"
	p.user = user
	p.id = id
	p.input = in
	if p.fail != nil {
		return "", p.fail
	}
	return "Example", nil
}
func (p *applicationMutatorProbe) Delete(_ context.Context, user string, id int64) (string, error) {
	p.calls++
	p.method = "DELETE"
	p.user = user
	p.id = id
	if p.fail != nil {
		return "", p.fail
	}
	return "Example", nil
}

type companyApplicationProbe struct {
	calls         int
	user, company string
	fail          error
}

func (p *companyApplicationProbe) CompanyApplications(_ context.Context, user, company string) (applications.CompanyResponse, error) {
	p.calls++
	p.user = user
	p.company = company
	if p.fail != nil {
		return applications.CompanyResponse{}, p.fail
	}
	return applications.CompanyResponse{CompanyName: company, Limit: 10, Records: []applications.Record{}, Vacancies: []vacancies.CompanyVacancy{}}, nil
}
func TestCompanyApplicationReadAndMutations(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := m.Authenticate("demo", "demo-password")
	token, _ := m.Issue(user, time.Now())
	writer := &applicationMutatorProbe{}
	reader := &companyApplicationProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: m, ApplicationCompanies: reader, ApplicationWrites: writer})
	send := func(method, path, body, origin string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// No unauthorized user can enumerate other users' CRM records.
	if w := send("GET", "/api/applications?company=Example", "", "", false); w.Code != 401 || reader.calls != 0 {
		t.Fatal(w.Code)
	}
	if w := send("GET", "/api/applications?company=Example", "", "", true); w.Code != 200 || reader.user != "Demo" {
		t.Fatal(w.Code, w.Body.String())
	}
	validBody := `{"revision":"` + strings.Repeat("a", 64) + `","vacancyId":null,"status":"Скрининг","comment":"тест","applicationSources":["hh"],"id":4,"history":[],"companyName":"Example"}`
	if w := send("PUT", "/api/applications/4", validBody, "https://attacker.example", true); w.Code != 403 || writer.calls != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("PUT", "/api/applications/4", validBody, "", false); w.Code != 401 || writer.calls != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("PUT", "/api/applications/nope", validBody, "", true); w.Code != 400 || writer.calls != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{`, `{"revision":"` + strings.Repeat("a", 64) + `"} trailing`, strings.Repeat("a", 33<<10)} {
		if w := send("PUT", "/api/applications/4", body, "", true); w.Code != 400 || writer.calls != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := send("PUT", "/api/applications/4", validBody, "", true); w.Code != 200 || writer.calls != 1 || reader.calls != 2 || writer.user != "Demo" || writer.id != 4 || writer.input.Status != "Скрининг" {
		t.Fatal(w.Code, w.Body.String(), writer)
	} else {
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["companyName"] != "Example" || response["limit"] != float64(10) || response["vacancies"] == nil || response["records"] == nil {
			t.Fatal(w.Body.String(), err)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{applications.ErrNotFound, 404}, {applications.ErrRevisionRequired, 428},
		{applications.ErrConflict, 409}, {applications.ErrDuplicateVacancy, 400},
		{applications.ErrInvalidVacancy, 400}, {applications.ErrInvalidInput, 400},
		{errors.New("DB_PASSWORD=hidden"), 503},
	} {
		writer.fail = tc.err
		reads := reader.calls
		w := send("PUT", "/api/applications/4", validBody, "", true)
		if w.Code != tc.status || reader.calls != reads || strings.Contains(w.Body.String(), "DB_PASSWORD") {
			t.Fatal(tc.err, w.Code, w.Body.String())
		}
	}
	writer.fail = nil
	if w := send("DELETE", "/api/applications/4", "", "", true); w.Code != 200 || writer.method != "DELETE" || reader.company != "Example" {
		t.Fatal(w.Code, w.Body.String())
	}
	reader.fail = errors.New("db failed after commit")
	if w := send("DELETE", "/api/applications/4", "", "", true); w.Code != 503 || !strings.Contains(w.Body.String(), "Изменение выполнено") {
		t.Fatal(w.Code, w.Body.String())
	}
	reader.fail = applications.ErrCompanyNotFound
	if w := send("GET", "/api/applications?company=Unknown", "", "", true); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMutationRoutesRequireBothDependencies(t *testing.T) {
	m, _ := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "password"})
	u, _ := m.Authenticate("demo", "password")
	token, _ := m.Issue(u, time.Now())
	h := NewHandlerWithOptions(nil, Options{Auth: m, ApplicationCompanies: &companyApplicationProbe{}})
	r := httptest.NewRequest("DELETE", "/api/applications/8", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("unmigrated writes must fail closed, got %d", w.Code)
	}
}
