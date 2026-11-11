package workerconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"itmap/internal/parser"
)

func TestErrorCodeNeverIncludesErrorText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{fmt.Errorf("synthetic secret: %w", parser.ErrRunBusy), "scheduler_busy"},
		{fmt.Errorf("synthetic secret: %w", parser.ErrPipelinePartial), "pipeline_partial"},
		{fmt.Errorf("synthetic secret: %w", parser.ErrJobLeaseLost), "lease_lost"},
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{errors.New("synthetic secret"), "worker_error"},
	} {
		got := ErrorCode(tc.err)
		if got != tc.want || strings.Contains(got, "secret") {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}
