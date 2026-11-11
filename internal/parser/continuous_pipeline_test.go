package parser

import (
	"context"
	"errors"
	"testing"
)

func TestTryPipelineSessionRequiresDatabase(t *testing.T) {
	worked, err := (CycleControl{}).TryPipelineSession(context.Background())
	if worked || !errors.Is(err, ErrSettingsStorage) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
}
