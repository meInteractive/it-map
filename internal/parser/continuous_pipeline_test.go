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

func TestPipelineSessionReportsPartialFailure(t *testing.T) {
	worked, err := pipelineSessionResult(WorkerResult{Run: Run{Status: "partial"}, Processed: 1}, nil)
	if !worked || !errors.Is(err, ErrPipelinePartial) {
		t.Fatalf("partial session: worked=%v err=%v", worked, err)
	}
	worked, err = pipelineSessionResult(WorkerResult{Run: Run{Status: "completed"}, Processed: 1}, nil)
	if !worked || err != nil {
		t.Fatalf("completed session: worked=%v err=%v", worked, err)
	}
}
