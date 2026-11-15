package apifyruns

import (
	"context"
	"errors"
	"itmap/internal/integrations/apify"
	"testing"
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
		{&apify.Error{Code: "apify_data", Status: 502}, false},
		{errors.Join(&apify.Error{Code: "apify_network"}, errors.New("cleanup")), false},
		{context.Canceled, false},
	} {
		if got := errors.Is(classifyReadError(tc.in), ErrReadTemporary); got != tc.retry {
			t.Fatalf("classification %v = %v", tc.in, got)
		}
	}
}
