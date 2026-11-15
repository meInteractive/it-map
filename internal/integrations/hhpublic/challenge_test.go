package hhpublic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVerificationPageDoesNotTriggerPaidFallback(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		for _, body := range []string{`<div class="g-recaptcha"></div>`, `<div class="smartcaptcha"></div>`, `<h1>Подтвердите, что вы не робот</h1>`} {
			client := New()
			client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}, "Retry-After": {"180"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123")
			var hint *TemporaryError
			if job != nil || !errors.As(err, &hint) || hint.RetryAfter != 3*time.Minute || !errors.Is(err, ErrTemporary) {
				t.Fatalf("status %d did not defer: %v", status, err)
			}
		}
	}
}

func TestVerificationMarkersDoNotHideValidJobOrChangeOther403(t *testing.T) {
	client := New()
	body := fixtureJob("CI title", "CI description mentioning g-recaptcha")
	client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123"); err != nil || job == nil {
		t.Fatal("valid posting was hidden", err)
	}
	client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("access denied")), Request: r}, nil
	})
	if job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123"); err != nil || job != nil {
		t.Fatal("ordinary 403 changed", err)
	}
}

func TestInactiveCaptchaScriptDoesNotBlockFallback(t *testing.T) {
	client := New()
	client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(`<script src="https://static.invalid/smartcaptcha.js"></script>`)), Request: r}, nil
	})
	if job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123"); job != nil || err != nil {
		t.Fatal("inactive script blocked fallback", err)
	}
}

func TestForbiddenUnreadableBodyDoesNotTriggerFallback(t *testing.T) {
	for _, body := range []io.ReadCloser{brokenBody{}, io.NopCloser(strings.NewReader(strings.Repeat("x", maxBytes+1)))} {
		client := New()
		client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"text/html"}}, Body: body, Request: r}, nil
		})
		if job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123"); job != nil || !errors.Is(err, ErrTemporary) {
			t.Fatal("unreadable forbidden response enabled fallback", err)
		}
	}
}
