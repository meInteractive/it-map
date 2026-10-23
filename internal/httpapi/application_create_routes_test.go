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
)

type creationProbe struct {
	contextCalls, createCalls int
	user                      string
	input                     applications.CreateInput
	result                    applications.CreationResult
	err                       error
}

func (p *creationProbe) CreationContext(_ context.Context, user string) (string, error) {
	p.contextCalls++
	p.user = user
	return "ac1." + strings.Repeat("A", 43), p.err
}
func (p *creationProbe) CreateWithReceipt(_ context.Context, user string, in applications.CreateInput) (applications.CreationResult, error) {
	p.createCalls++
	p.user = user
	p.input = in
	return p.result, p.err
}

func TestApplicationCreationRoutes(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-secret"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := manager.Authenticate("demo", "demo-secret")
	token, _ := manager.Issue(u, time.Now())
	p := &creationProbe{result: applications.CreationResult{CreatedApplicationID: 7}}
	h := NewHandlerWithOptions(nil, Options{Auth: manager, ApplicationCreates: p})
	req := func(method, path, body, mime, origin string, signed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://itmap.test"+path, strings.NewReader(body))
		if signed {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if mime != "" {
			r.Header.Set("Content-Type", mime)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := req("GET", "/api/applications/create-context", "", "", "", false); w.Code != 401 || p.contextCalls != 0 {
		t.Fatal(w.Code)
	}
	if w := req("GET", "/api/applications/create-context", "", "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"creationContext"`) || p.user != "Demo" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		method, path, body, mime, origin string
		authorized                       bool
		status                           int
	}{
		{"POST", "/api/applications/create", `{}`, "application/json", "", false, 401},
		{"POST", "/api/applications/create", `{}`, "application/json", "https://evil.test", true, 403},
		{"POST", "/api/applications/create", `{}`, "text/plain", "", true, 400},
		{"POST", "/api/applications/create", `{`, `application/json`, "", true, 400},
		{"POST", "/api/applications/create", `[]`, "application/json", "", true, 400},
		{"POST", "/api/applications/create", `{} {}`, "application/json", "", true, 400},
		{"POST", "/api/applications/create", strings.Repeat(" ", 32<<10) + `{}`, "application/json", "", true, 400},
		{"POST", "/api/applications/create", `{"companyName":"Example","status":"Откликнулся"}`, "application/json", "https://itmap.test", true, 201},
		{"POST", "/api/applications", `{"companyName":"Example"}`, "application/json", "", true, 201},
	} {
		w := req(tc.method, tc.path, tc.body, tc.mime, tc.origin, tc.authorized)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if p.createCalls != 2 || p.user != "Demo" {
		t.Fatal(p)
	}
	p.result.Replayed = true
	if w := req("POST", "/api/applications/create", `{}`, "application/json", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{applications.ErrCreationContextRequired, 428, "creation_context_required"},
		{applications.ErrCreationContextExpired, 409, "creation_context_expired"},
		{applications.ErrIntentConflict, 409, "intent_conflict"},
		{applications.ErrApplicationDeleted, 410, "application_deleted"},
		{applications.ErrIncompleteReceipt, 503, "incomplete_receipt"},
		{applications.ErrApplicationLimit, 400, ""},
		{applications.ErrDuplicateVacancy, 400, ""},
		{applications.ErrInvalidVacancy, 400, ""},
		{applications.ErrCompanyNotFound, 404, ""},
		{applications.ErrInvalidInput, 400, ""},
		{errors.New("DB_PASSWORD=this-is-secret"), 503, ""},
	} {
		p.err = tc.err
		w := req("POST", "/api/applications/create", `{}`, "application/json", "", true)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "DB_PASSWORD") || (tc.code != "" && !strings.Contains(w.Body.String(), tc.code)) {
			t.Fatalf("%v: %d %s", tc.err, w.Code, w.Body.String())
		}
	}
	p.err = errors.New("secret-access-token")
	if w := req("GET", "/api/applications/create-context", "", "", "", true); w.Code != 503 || strings.Contains(w.Body.String(), "secret-access-token") {
		t.Fatal(w.Code, w.Body.String())
	}
}
