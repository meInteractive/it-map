package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

type catalogRatingProbe struct{ calls int }

func (p *catalogRatingProbe) RecalculateRating(context.Context) (catalog.RatingResult, error) {
	p.calls++
	return catalog.RatingResult{OK: true, Companies: 1}, nil
}

func TestCatalogRatingRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("r", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	probe := &catalogRatingProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CatalogRatings: probe})
	for _, tc := range []struct {
		method, user, origin string
		status               int
	}{
		{"POST", "", "", 401}, {"POST", "demo", "", 403}, {"POST", "eugene", "https://other.invalid", 403},
		{"GET", "eugene", "", 405}, {"POST", "eugene", "https://itmap.test", 200},
	} {
		r := httptest.NewRequest(tc.method, "https://itmap.test/api/companies/recalculate-rating", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.user != "" {
			user, _ := manager.Authenticate(tc.user, "synthetic")
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("HTTP %s %s: %d, ожидается %d", tc.method, tc.user, w.Code, tc.status)
		}
	}
	if probe.calls != 1 {
		t.Fatal("пересчёт вызван для отклонённого запроса")
	}
}
