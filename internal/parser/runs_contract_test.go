package parser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestSchedulerRunLegacyIdentity(t *testing.T) {
	data, err := os.ReadFile("testdata/run-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		DefaultDurationMS, LeaseSlackMS int64
		Vectors                         []struct{ Generation, RequestID, ID string }
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Vectors) != 3 || fixture.DefaultDurationMS != 110000 || fixture.LeaseSlackMS != 30000 {
		t.Fatal("legacy run lease fixture missing")
	}
	for _, v := range fixture.Vectors {
		if runID(v.Generation, v.RequestID) != v.ID {
			t.Fatal("run identity differs from legacy JS")
		}
	}
}
func TestSchedulerRunRejectsInvalidInputBeforeStorage(t *testing.T) {
	r := RunRepository{}
	for _, v := range []RunOptions{{}, {RequestID: "CI synthetic", Trigger: "CI unknown"}, {RequestID: "CI synthetic", Duration: -time.Second}, {RequestID: "CI synthetic", Duration: 6 * time.Minute}} {
		if _, err := r.Begin(context.Background(), v); !errors.Is(err, ErrRunInput) {
			t.Fatal("invalid run input not rejected before storage")
		}
	}
}
