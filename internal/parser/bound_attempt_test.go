package parser

import (
	"context"
	"errors"
	"testing"
)

func TestBoundAttemptRejectsUnboundHandles(t *testing.T) {
	var b *BoundRun
	if _, err := b.StartBoundAttempt(context.Background(), nil); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound attempt accepted", err)
	}
	var a *BoundAttempt
	if err := a.Reserve(context.Background(), []byte(`{}`)); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound reservation accepted", err)
	}
	if err := a.Interrupt(context.Background()); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound interruption accepted", err)
	}
	a = &BoundAttempt{bound: &BoundRun{}, lease: &JobLease{}, id: "synthetic"}
	for _, request := range []string{`{}`, `{"provider":"apify","model":"synthetic","free":true,"capUsd":0.26}`, `{"provider":"apify","model":"synthetic","capUsd":0.27}`} {
		if err := a.Reserve(context.Background(), []byte(request)); !errors.Is(err, ErrBudgetUnknown) {
			t.Fatal("invalid or zero-cost reservation accepted", err)
		}
	}
}
