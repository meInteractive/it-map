package parser

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestBoundLeaseRefusesWorkUntilConsumerFenceIsIntegrated(t *testing.T) {
	lease := &JobLease{owner: "synthetic", jobID: "synthetic", consumerBound: true}
	if _, err := lease.StartAttempt(context.Background()); !errors.Is(err, ErrBoundJobUnavailable) {
		t.Fatal("attempt started", err)
	}
	called := false
	if err := lease.WithinOwned(context.Background(), func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, ErrBoundJobUnavailable) || called {
		t.Fatal("callback executed", err)
	}
	if err := lease.AssertActive(context.Background()); !errors.Is(err, ErrBoundJobUnavailable) {
		t.Fatal("external work allowed", err)
	}
	if err := lease.Finish(context.Background(), FinishOptions{Payload: []byte(`{}`)}); !errors.Is(err, ErrBoundJobUnavailable) {
		t.Fatal("finish allowed", err)
	}
}
