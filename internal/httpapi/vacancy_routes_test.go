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
	"itmap/internal/vacancies"
)

type vacancyProbe struct {
	calls  int
	user   string
	period *vacancies.Period
	when   time.Time
	err    error
}

func (p *vacancyProbe) List(_ context.Context, user string, period *vacancies.Period, when time.Time) (vacancies.ListResponse, error) {
	p.calls++
	p.user = user
	p.period = period
	p.when = when
	if p.err != nil {
		return vacancies.ListResponse{}, p.err
	}
	return vacancies.ListResponse{Vacancies: []vacancies.Vacancy{{ID: 9, CompanyName: "Компания", EntityType: "company", Title: "Руководитель", Roles: nil}}, Entities: []vacancies.Entity{}, ApplicationSummary: []vacancies.ApplicationSummary{}}, nil
}
func TestVacancyListAuthenticatedPeriodAndErrorContract(t *testing.T) {
	authManager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-test-password", "demo": "demoPassword"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := authManager.Authenticate("demo", "demoPassword")
	token, err := authManager.Issue(u, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := &vacancyProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: authManager, Vacancies: p})
	// The real handler wires time.Now. Fixed-time unit coverage is separate.
	call := func(method, path string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/vacancies/list?period=week", false); w.Code != http.StatusUnauthorized || p.calls != 0 {
		t.Fatalf("unauthenticated %d calls %d", w.Code, p.calls)
	}
	for _, path := range []string{"/api/vacancies/list?period=", "/api/vacancies/list?period=year", "/api/vacancies/list?period=ALL"} {
		if w := call("GET", path, true); w.Code != 400 || p.calls != 0 {
			t.Fatalf("invalid %s %d calls %d", path, w.Code, p.calls)
		}
	}
	if w := call("GET", "/api/vacancies/list?period=all", true); w.Code != 200 || p.calls != 1 || p.user != "Demo" || p.period == nil || *p.period != vacancies.All || !strings.Contains(w.Body.String(), `"vacancies"`) {
		t.Fatalf("all %d body %s state %+v", w.Code, w.Body.String(), p)
	}
	if w := call("GET", "/api/vacancies/list", true); w.Code != 200 || p.period != nil || p.calls != 2 {
		t.Fatalf("no period: %d", w.Code)
	}
	if w := call("GET", "/api/vacancies/9", true); w.Code != 503 || p.calls != 2 {
		t.Fatalf("unported vacancy detail: %d", w.Code)
	}
	if w := call("POST", "/api/vacancies/list", true); w.Code == 200 || p.calls != 2 {
		t.Fatal("write route accidentally enabled")
	}
	p.err = errors.New("DB_PASSWORD=should-never-leak")
	if w := call("GET", "/api/vacancies/list", true); w.Code != 503 || strings.Contains(w.Body.String(), "should-never-leak") {
		t.Fatalf("leaked repository error: %d %s", w.Code, w.Body.String())
	}
}
