package httpapi

import (
	"context"
	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type apifySettingsProbe struct {
	writes     int
	generation bool
}

func (p *apifySettingsProbe) Read(context.Context) (apifysettings.PublicView, error) {
	return apifysettings.PublicSettings(apifysettings.StoredSettings{}), nil
}
func (p *apifySettingsProbe) Generation(context.Context) (string, error) {
	p.generation = true
	return "CI_GENERATION", nil
}
func (p *apifySettingsProbe) SaveKey(_ context.Context, g string, _ apifysettings.KeyInput) (apifysettings.PublicView, error) {
	if g != "CI_GENERATION" {
		return apifysettings.PublicView{}, apifysettings.ErrGeneration
	}
	p.writes++
	return apifysettings.PublicSettings(apifysettings.StoredSettings{}), nil
}
func (p *apifySettingsProbe) SaveSettings(_ context.Context, g string, _ apifysettings.SettingsInput) (apifysettings.PublicView, error) {
	if g != "CI_GENERATION" {
		return apifysettings.PublicView{}, apifysettings.ErrGeneration
	}
	p.writes++
	return apifysettings.PublicSettings(apifysettings.StoredSettings{}), nil
}
func (p *apifySettingsProbe) ReadHHSource(_ context.Context, id int64) (apifysettings.HHSource, error) {
	return apifysettings.HHSource{CompanyID: id, SourceID: "hh", URL: "https://hh.ru/employer/123"}, nil
}
func (p *apifySettingsProbe) SaveHHSource(_ context.Context, g string, id int64, _ apifysettings.HHSourceInput) (apifysettings.HHSource, error) {
	if g != "CI_GENERATION" {
		return apifysettings.HHSource{}, apifysettings.ErrGeneration
	}
	p.writes++
	return apifysettings.HHSource{CompanyID: id, SourceID: "hh"}, nil
}

func TestApifySettingsHTTPAccessAndValidation(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := func(login string) *http.Cookie {
		user, ok := m.Authenticate(login, map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"}[login])
		if !ok {
			t.Fatal("authenticate")
		}
		value, err := m.Issue(user, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.CookieName, Value: value}
	}
	p := &apifySettingsProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: m, ApifySettings: p})
	call := func(method, path, login, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
		if login != "" {
			r.AddCookie(cookie(login))
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	key := "apify_api_" + strings.Repeat("x", 16)
	for _, tc := range []struct {
		method, path, login, origin, body string
		status                            int
	}{
		{"GET", "/api/apify/settings", "", "", "", 401}, {"GET", "/api/apify/settings", "demo", "", "", 403},
		{"GET", "/api/apify/settings", "eugene", "", "", 200}, {"POST", "/api/apify/settings", "eugene", "", "{}", 405},
		{"PUT", "/api/apify/key", "eugene", "https://foreign.invalid", `{}`, 403},
		{"PUT", "/api/apify/key", "eugene", "https://example.com", `{"profile":"free","apiKey":"` + key + `","revision":0}`, 200},
		{"PUT", "/api/apify/key", "eugene", "", `{"profile":"free","apiKey":"bad","revision":0}`, 400},
		{"PUT", "/api/apify/settings", "eugene", "", `{"enabled":true,"paidAllowed":false,"revision":0}`, 200},
		{"GET", "/api/apify/hh-source?companyId=1", "demo", "", "", 403},
		{"GET", "/api/apify/hh-source?companyId=1", "eugene", "", "", 200},
		{"GET", "/api/apify/hh-source?companyId=1&other=1", "eugene", "", "", 400},
		{"PUT", "/api/apify/hh-source?companyId=1", "eugene", "https://example.com", `{"sourceUrl":"https://hh.ru/employer/123","enabled":true,"revision":0}`, 200},
		{"PUT", "/api/apify/hh-source?companyId=0", "eugene", "", `{"sourceUrl":"https://hh.ru/employer/123","enabled":true,"revision":0}`, 400},
		{"PUT", "/api/apify/key", "eugene", "", strings.Repeat("x", 4097), 413},
		{"PUT", "/api/firecrawl/sources/1/hh", "eugene", "https://example.com", `{"sourceUrl":"https://hh.ru/employer/123","enabled":true,"revision":0,"config":{}}`, 200},
		{"PUT", "/api/firecrawl/sources/1/hh", "demo", "", `{}`, 403},
		{"GET", "/api/firecrawl/sources/1/hh", "eugene", "", "", 405},
		{"GET", "/api/apify/check", "eugene", "", "", 404},
	} {
		w := call(tc.method, tc.path, tc.login, tc.origin, tc.body)
		if w.Code != tc.status || strings.Contains(w.Body.String(), key) {
			t.Fatalf("%s %s: status=%d expected=%d", tc.method, tc.path, w.Code, tc.status)
		}
	}
	if p.writes != 4 || !p.generation {
		t.Fatalf("unexpected writes=%d generation=%v", p.writes, p.generation)
	}
}
