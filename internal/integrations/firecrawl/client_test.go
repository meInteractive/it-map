package firecrawl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestScrape(t *testing.T) {
	ctx := context.Background()
	target := "https://careers.company.com/jobs/42"
	options := ScrapeOptions{AllowedHosts: []string{"careers.company.com"}, Timeout: time.Second, MarkdownOnly: true}
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		err     error
		credits *float64
	}{
		{"success", 200, `{"success":true,"data":{"markdown":"Job","metadata":{"creditsUsed":2}}}`, nil, credit(2)},
		{"unreported", 200, `{"success":true,"data":{"markdown":"Job"}}`, nil, nil},
		{"foreign final URL", 200, `{"success":true,"data":{"metadata":{"url":"https://foreign.company.com/","creditsUsed":1}}}`, ErrTarget, credit(1)},
		{"captcha", 200, `{"success":true,"data":{"markdown":"Access denied","metadata":{"creditsUsed":1}}}`, ErrTarget, credit(1)},
		{"provider redirect", 307, "", ErrRejected, nil},
		{"unknown network outcome", 0, "", ErrNetwork, nil},
		{"null data", 200, `{"success":true,"data":null}`, ErrResponse, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := New("synthetic", transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.String() != endpoint+"scrape" || r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Fatal("unexpected request")
				}
				if tc.status == 0 {
					return nil, errors.New("synthetic network failure")
				}
				h := make(http.Header)
				h.Set("Location", "https://foreign.company.com/")
				return &http.Response{StatusCode: tc.status, Header: h, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.Scrape(ctx, target, options)
			if !errors.Is(err, tc.err) || calls != 1 || result.ProviderStatus != tc.status {
				t.Fatalf("status=%d calls=%d err=%v", result.ProviderStatus, calls, err)
			}
			if tc.credits == nil && result.ReportedCredits != nil ||
				tc.credits != nil && (result.ReportedCredits == nil || *result.ReportedCredits != *tc.credits) {
				t.Fatalf("unexpected reported credits: %v", result.ReportedCredits)
			}
			if err == nil && (result.Markdown != "Job" || result.FinalURL != target) {
				t.Fatalf("unexpected content: %+v", result)
			}
		})
	}
}

func TestRejectTargetBeforeSending(t *testing.T) {
	c, err := New("synthetic", transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid target must not contact the provider")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "0x7f.0.0.1", "careers.internal", "careers.company.com:8443"} {
		_, err := c.Scrape(context.Background(), "https://"+host, ScrapeOptions{AllowedHosts: []string{host}, Timeout: time.Second})
		if !errors.Is(err, ErrInput) {
			t.Fatalf("%s: %v", host, err)
		}
	}
}

func TestCreditUsage(t *testing.T) {
	c, err := New("synthetic", transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.String() != endpoint+"team/credit-usage" {
			t.Fatal("unexpected credit usage request")
		}
		body := `{"success":true,"data":{"remainingCredits":0,"planCredits":1000}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.CreditUsage(context.Background())
	if err != nil || result.RemainingCredits == nil || *result.RemainingCredits != 0 {
		t.Fatalf("unexpected result: %+v, %v", result, err)
	}
}

func credit(v float64) *float64 { return &v }
