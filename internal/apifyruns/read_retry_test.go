package apifyruns

import (
	"context"
	"errors"
	"itmap/internal/integrations/apify"
	"testing"
	"time"
)

func TestReadFailureClassificationNeverTreatsUnknownAsSafe(t *testing.T) {
	for _, tc := range []struct {
		in    error
		retry bool
	}{
		{&apify.Error{Code: "apify_network", Status: 503}, true},
		{&apify.Error{Code: "apify_timeout", Status: 503}, true},
		{&apify.Error{Code: "apify_busy", Status: 503}, true},
		{&apify.Error{Code: "apify_auth", Status: 400}, false},
		{&apify.Error{Code: "apify_auth", Status: 400, RetryableRead: true}, false},
		{&apify.Error{Code: "apify_data", Status: 502}, false},
		{&apify.Error{Code: "apify_data", Status: 503, RetryableRead: true}, true},
		{errors.Join(&apify.Error{Code: "apify_network"}, errors.New("cleanup")), false},
		{context.Canceled, false},
	} {
		if got := errors.Is(classifyReadError(tc.in), ErrReadTemporary); got != tc.retry {
			t.Fatalf("classification %v = %v", tc.in, got)
		}
	}
}

func TestReadRetryHintIsBoundedAndPreservesSentinel(t *testing.T) {
	err := classifyReadError(&apify.Error{Code: "apify_busy", Status: 503, RetryAfter: 3 * time.Minute})
	var hint *ReadTemporaryError
	if !errors.As(err, &hint) || hint.RetryAfter != 3*time.Minute || !errors.Is(err, ErrReadTemporary) {
		t.Fatal("hint lost", err)
	}
	err = classifyReadError(&apify.Error{Code: "apify_busy", Status: 503, RetryAfter: 25 * time.Hour})
	if err != ErrReadTemporary {
		t.Fatal("unbounded retry accepted", err)
	}
}
