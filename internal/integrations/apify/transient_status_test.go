package apify

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestTransientStatusReadAndUncertainPost(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly} {
		client, calls := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "180")
			w.WriteHeader(status)
		})
		_, err := client.Run(context.Background(), "Run123")
		var e *Error
		if !errors.As(err, &e) || e.Code != "apify_data" || !e.RetryableRead || e.RetryAfter != 3*time.Minute {
			t.Fatalf("GET %d: %v", status, err)
		}
		_, err = client.Start(context.Background(), "https://hh.ru/employer/1", HHOptions{})
		if !IsStartUncertain(err) || !errors.As(err, &e) || e.Code != "apify_data" || e.RetryableRead || e.RetryAfter != 0 || calls.Load() != 2 {
			t.Fatalf("POST %d: %v", status, err)
		}
	}
}

func TestTransientReadUsesProviderClock(t *testing.T) {
	server := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	client, _ := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", server.Format(http.TimeFormat))
		w.Header().Set("Retry-After", server.Add(3*time.Minute).Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Run(context.Background(), "Run123")
	var hint *Error
	if !errors.As(err, &hint) || hint.RetryAfter != 3*time.Minute {
		t.Fatalf("unexpected error: %v", err)
	}
}
