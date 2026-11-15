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

func TestTransientStatusRespectsRetryAfter(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly} {
		client := New()
		client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"180"}}, Body: io.NopCloser(strings.NewReader("private")), Request: r}, nil
		})
		job, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123")
		var hint *TemporaryError
		if job != nil || !errors.As(err, &hint) || hint.RetryAfter != 3*time.Minute || !errors.Is(err, ErrTemporary) || strings.Contains(err.Error(), "private") {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}

func TestTransientStatusUsesProviderClock(t *testing.T) {
	server := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	client := New()
	client.http.Transport = localTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {server.Add(3 * time.Minute).Format(http.TimeFormat)}, "Date": {server.Format(http.TimeFormat)}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, err := client.Fetch(context.Background(), "https://hh.ru/vacancy/123")
	var hint *TemporaryError
	if !errors.As(err, &hint) || hint.RetryAfter != 3*time.Minute {
		t.Fatalf("unexpected error: %v", err)
	}
}
