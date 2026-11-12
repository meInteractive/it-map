package parser

import (
	"context"
	"errors"
	"testing"
)

func TestReleaseUnstartedRejectsUnboundLeases(t *testing.T) {
	var b *BoundRun
	if err := b.ReleaseUnstarted(context.Background(), nil); !errors.Is(err, ErrJobInput) {
		t.Fatal("nil bound run accepted", err)
	}
	b = &BoundRun{}
	if err := b.ReleaseUnstarted(context.Background(), &JobLease{}); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound job accepted", err)
	}
}
