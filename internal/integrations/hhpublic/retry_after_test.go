package hhpublic

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterBoundsAndFormats(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"", time.Minute}, {"-1", time.Minute}, {"0", time.Minute}, {"90", 90 * time.Second},
		{"9999999999999999999", 24 * time.Hour}, {"99999999999999999999999999", 24 * time.Hour}, {now.Add(2 * time.Hour).Format(http.TimeFormat), 2 * time.Hour},
		{now.Add(-time.Hour).Format(http.TimeFormat), time.Minute}, {now.Add(48 * time.Hour).Format(http.TimeFormat), 24 * time.Hour},
	} {
		if got := retryAfter(tc.raw, now); got != tc.want {
			t.Fatalf("header %q: got %v want %v", tc.raw, got, tc.want)
		}
	}
	if !errors.Is(&TemporaryError{RetryAfter: time.Minute}, ErrTemporary) {
		t.Fatal("lost safe classification")
	}
}
