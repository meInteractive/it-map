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
	"itmap/internal/channels"
)

type channelsProbe struct {
	Calls   int
	Failure error
}

func (p *channelsProbe) List(_ context.Context) ([]channels.Channel, error) {
	p.Calls++
	if p.Failure != nil {
		return nil, p.Failure
	}
	return []channels.Channel{{ID: 4, Name: "Тестовый канал", URL: "https://t.me/test_channel", Username: "@test_channel", NormalizedUsername: "test_channel", VacancyCount: 3, Comment: "", CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02"}}, nil
}
func TestChannelsListIsAuthenticatedAndReadOnly(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("d", 64), map[string]string{"eugene": "password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("eugene", "password")
	token, _ := m.Issue(u, time.Now())
	probe := &channelsProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, Channels: probe})
	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, req)
	if unauth.Code != 401 || probe.Calls != 0 {
		t.Fatalf("unauthorized call: %d, calls=%d", unauth.Code, probe.Calls)
	}
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	authorized := httptest.NewRecorder()
	h.ServeHTTP(authorized, req)
	if authorized.Code != 200 || !strings.Contains(authorized.Body.String(), `"vacancyCount":3`) || !strings.Contains(authorized.Body.String(), `"lastSuccessfulCheckAt":null`) {
		t.Fatalf("invalid response %d %s", authorized.Code, authorized.Body.String())
	}
	if probe.Calls != 1 {
		t.Fatalf("wrong query count: %d", probe.Calls)
	}
	probe.Failure = errors.New("sensitive DB credentials")
	unavailable := httptest.NewRecorder()
	h.ServeHTTP(unavailable, req)
	if unavailable.Code != 503 || strings.Contains(unavailable.Body.String(), "credentials") {
		t.Fatalf("error leaked: %d %s", unavailable.Code, unavailable.Body.String())
	}
	// The v316 POST is deliberately unported: no accidental mutation.
	post := httptest.NewRequest(http.MethodPost, "/api/channels", nil)
	post.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	unsupported := httptest.NewRecorder()
	h.ServeHTTP(unsupported, post)
	if unsupported.Code == 200 || unsupported.Code == 201 || probe.Calls != 2 {
		t.Fatalf("unported mutation accepted: %d", unsupported.Code)
	}
}
