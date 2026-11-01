package hhpublic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type localTransport func(*http.Request) (*http.Response, error)

func (f localTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPublicHHHTTPBoundsAndNoRedirect(t *testing.T) {
	var calls atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/vacancy/123" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "text/html" {
			t.Error("unsafe public request")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, fixtureJob("CI title", "CI description"))
	}))
	defer peer.Close()
	target, _ := url.Parse(peer.URL)
	client := New()
	client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "hh.ru" {
			t.Error("arbitrary request host")
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		copy.URL = &u
		return http.DefaultTransport.RoundTrip(copy)
	})
	got, err := client.Fetch(context.Background(), "https://spb.hh.ru/vacancy/123?x=1#y")
	if err != nil || got == nil || got.ID != "123" || calls.Load() != 1 {
		t.Fatal("public HTTP result", got, err)
	}
	if got, err = client.Fetch(context.Background(), "https://ci.invalid/vacancy/123"); err != nil || got != nil || calls.Load() != 1 {
		t.Fatal("invalid scope sent request", err)
	}
	for _, tc := range []struct {
		status            int
		contentType, body string
	}{{302, "text/html", "CI redirect"}, {200, "application/json", "{}"}, {200, "text/html", strings.Repeat("x", 2000001)}, {404, "text/html", "CI missing"}} {
		client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}, "Location": {"https://ci.invalid/"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
		})
		before := calls.Load()
		got, err = client.Fetch(context.Background(), "https://hh.ru/vacancy/123")
		if err != nil || got != nil || calls.Load() != before+1 {
			t.Fatal("unsafe/missing public body accepted or redirect retried", tc.status, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	if _, err = client.Fetch(ctx, "https://hh.ru/vacancy/123"); err != context.Canceled || calls.Load() != before {
		t.Fatal("cancelled request sent network", err)
	}
}
