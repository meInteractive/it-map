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
	if err := a.Interrupt(context.Background()); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound interruption accepted", err)
	}
}
