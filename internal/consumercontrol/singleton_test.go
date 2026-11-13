package consumercontrol

import (
	"context"
	"errors"
	"testing"
)

func TestGuardSingletonRejectsInvalidArguments(t *testing.T) {
	if err := GuardSingleton(context.Background(), nil, testGeneration); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if err := GuardSingleton(nil, nil, testGeneration); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
}
