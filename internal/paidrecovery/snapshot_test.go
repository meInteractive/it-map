package paidrecovery

import (
	"context"
	"errors"
	"testing"
)

func TestSnapshotRejectsUnavailableInputs(t *testing.T) {
	if _, err := Inspect(nil, nil, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing input accepted", err)
	}
	if _, err := Inspect(context.Background(), nil, "11111111-2222-4333-8444-555555555555"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing DB accepted", err)
	}
}
