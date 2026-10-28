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
)

type testCatalog struct {
	query    string
	queryRaw json.RawMessage
	broken   bool
}

func (c *testCatalog) SearchCompanies(_ context.Context, q json.RawMessage) (json.RawMessage, error) {
	c.queryRaw = q
	_ = json.Unmarshal(q, &c.query)
	if c.broken {
		return nil, errors.New("private internal SQL error: credentials must never leak")
	}
	return json.RawMessage(`[{"name":"Тестовая компания","sector":"IT"}]`), nil
}
func TestCompanySearchRequiresSessionAndPreservesResponse(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("a", 40), map[string]string{"eugene": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("eugene", "pw")
	token, _ := m.Issue(u, time.Now())
	fake := &testCatalog{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, Catalog: fake})
	send := func(q string, authorized bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/companies/search?q="+q, nil)
		if authorized {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := send("abc", false); w.Code != 401 {
		t.Fatalf("unauthorized query=%d", w.Code)
	}
	if fake.query != "" {
		t.Fatal("search executed without session")
	}
	if w := send("%D0%9F%D0%BE%D0%B8%D1%81%D0%BA", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"matches":[{"name":"Тестовая компания","sector":"IT"}]`) {
		t.Fatalf("authorized query=%d %q", w.Code, w.Body.String())
	}
	if fake.query != "Поиск" {
		t.Fatalf("lost Unicode query %q", fake.query)
	}
	if w := send(strings.Repeat("x", 300), true); w.Code != 200 || len(fake.query) != 256 {
		t.Fatalf("limit failed %d, %d", w.Code, len(fake.query))
	}
	if w := send(strings.Repeat("x", 255)+"%F0%9F%98%80", true); w.Code != 200 || string(fake.queryRaw) != `"`+strings.Repeat("x", 255)+`\ud83d"` {
		t.Fatalf("source UTF16 cap lost: %s", fake.queryRaw)
	}
	fake.broken = true
	if w := send("hello", true); w.Code != 503 || strings.Contains(w.Body.String(), "credentials") {
		t.Fatalf("leaked repository error: %d %s", w.Code, w.Body.String())
	}
}
