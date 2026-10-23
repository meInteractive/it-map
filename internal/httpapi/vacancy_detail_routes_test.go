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

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type detailProbe struct {
	calls int
	user  string
	id    int64
	err   error
}

func (p *detailProbe) DetailByID(_ context.Context, user string, id int64) (vacancies.DetailResponse, error) {
	p.calls++
	p.user = user
	p.id = id
	if p.err != nil {
		return vacancies.DetailResponse{}, p.err
	}
	return vacancies.DetailResponse{Vacancy: vacancies.Detail{Vacancy: vacancies.Vacancy{ID: id, CompanyName: "Sample", Title: "Tech lead"}, URL: "https://example.org/role", DescriptionMD: "Description", CoverLetterMD: "private letter", CoverLetterScope: "personal", Revision: strings.Repeat("f", 64), TelegramSources: []vacancies.DetailTelegramSource{}, PublicationSources: []vacancies.PublicationSource{}}, Entities: []vacancies.Entity{}}, nil
}
func TestVacancyDetailAuthenticationAndContract(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-secret", "demo": "demoPassword"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "demoPassword")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	probe := &detailProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyDetails: probe})
	call := func(method, path string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/vacancies/35", false); w.Code != 401 || probe.calls != 0 {
		t.Fatalf("not protected %d", w.Code)
	}
	for _, path := range []string{"/api/vacancies/0", "/api/vacancies/notanid", "/api/vacancies/9223372036854775808"} {
		if w := call("GET", path, true); w.Code != 400 || probe.calls != 0 {
			t.Fatalf("invalid id %s: %d", path, w.Code)
		}
	}
	w := call("GET", "/api/vacancies/35", true)
	if w.Code != 200 || probe.calls != 1 || probe.user != "Demo" || probe.id != 35 {
		t.Fatalf("unexpected %d %+v", w.Code, probe)
	}
	var output map[string]json.RawMessage
	if err = json.Unmarshal(w.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Vacancy struct {
			ID          int64  `json:"id"`
			Revision    string `json:"revision"`
			Description string `json:"descriptionMd"`
			Scope       string `json:"coverLetterScope"`
		} `json:"vacancy"`
		Entities []any `json:"entities"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Vacancy.ID != 35 || detail.Vacancy.Description != "Description" || detail.Vacancy.Scope != "personal" || len(detail.Vacancy.Revision) != 64 || detail.Entities == nil {
		t.Fatalf("invalid JSON %s", w.Body.String())
	}
	probe.err = vacancies.ErrNotFound
	if w = call("GET", "/api/vacancies/36", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	probe.err = errors.New("SECRET=do-not-leak")
	if w = call("GET", "/api/vacancies/36", true); w.Code != 503 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("error leaked %s", w.Body.String())
	}
	if w = call("PUT", "/api/vacancies/35", true); w.Code != 503 {
		t.Fatalf("unsafe write enabled: %d", w.Code)
	}
}
