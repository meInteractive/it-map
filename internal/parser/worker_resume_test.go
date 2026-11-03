package parser

import (
	"errors"
	"testing"
)

func TestResumeModeNeverAdmitsActorStart(t *testing.T) {
	for _, starts := range []int32{0, 1} {
		if err := admitHHActorStart(true, starts); !errors.Is(err, ErrHHCycleLimit) {
			t.Fatalf("resume admitted an Actor with %d starts: %v", starts, err)
		}
	}
	if err := admitHHActorStart(false, 0); err != nil {
		t.Fatalf("ordinary first Actor rejected: %v", err)
	}
	if err := admitHHActorStart(false, 1); !errors.Is(err, ErrHHCycleLimit) {
		t.Fatalf("ordinary second Actor admitted: %v", err)
	}
}
