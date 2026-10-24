package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"itmap/internal/auth"
)

func TestLoginSessionAndLogout(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-secret", "nastya": "user-secret"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager})
	request := func(method, path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		var body string
		if values != nil {
			body = values.Encode()
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if values != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/session", nil, nil); w.Code != 401 {
		t.Fatalf("unauthenticated session=%d", w.Code)
	}
	if w := request("GET", "/api/vacancies/list", nil, nil); w.Code != 401 {
		t.Fatalf("unauthenticated API=%d", w.Code)
	}
	if w := request("POST", "/auth", url.Values{"login": {"eugene"}, "password": {"bad"}}, nil); w.Code != 401 || w.Header().Get("X-App-Login-Error") != "1" {
		t.Fatalf("invalid login=%d", w.Code)
	}
	good := request("POST", "/auth", url.Values{"login": {"Eugene"}, "password": {"admin-secret"}}, nil)
	if good.Code != 303 || good.Header().Get("Location") != "/" {
		t.Fatalf("login=%d, location=%s", good.Code, good.Header().Get("Location"))
	}
	var session *http.Cookie
	for _, c := range good.Result().Cookies() {
		if c.Name == auth.CookieName {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode || session.MaxAge != 2592000 {
		t.Fatalf("invalid session cookie: %+v", session)
	}
	if w := request("GET", "/api/session", nil, session); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Eugene"`) || !strings.Contains(w.Body.String(), `"isAdmin":true`) {
		t.Fatalf("valid session=%d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/vacancies/list", nil, session); w.Code != 503 {
		t.Fatalf("unported API unexpectedly available: %d", w.Code)
	}
	loggedOut := request("GET", "/logout", nil, session)
	if loggedOut.Code != 303 || !strings.Contains(loggedOut.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout not clearing cookie %s", loggedOut.Header().Get("Set-Cookie"))
	}
	if w := request("GET", "/mcp", nil, session); w.Code != 503 {
		t.Fatalf("internal MCP exposed: %d", w.Code)
	}
}

func TestLoginRejectsCrossOriginAndOversizedForms(t *testing.T) {
	manager, _ := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "secret"})
	h := NewHandlerWithOptions(nil, Options{Auth: manager})
	for _, origin := range []string{"https://evil.example", "https://127.0.0.1:80", "null"} {
		r := httptest.NewRequest("POST", "http://itmap.local/auth", strings.NewReader("login=eugene&password=secret"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin %q accepted: %d", origin, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/auth", strings.NewReader("login=eugene&password="+strings.Repeat("x", 9000)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("large payload status=%d", w.Code)
	}
}
