package parser

import (
	"errors"
	"fmt"
	"itmap/internal/apifyruns"
	"testing"
)

func TestActorAdmissionWaitDoesNotMaskCleanupFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{apifyruns.ErrBudget, true},
		{errors.Join(apifyruns.ErrBudget, nil), true},
		{fmt.Errorf("stage: %w", apifyruns.ErrBudget), true},
		{errors.Join(apifyruns.ErrBudget, errors.New("cleanup failure")), false},
		{apifyruns.ErrBusy, false},
		{nil, false},
	} {
		if got := onlyAdmissionCause(tc.err, apifyruns.ErrBudget); got != tc.want {
			t.Fatalf("classification %v = %v", tc.err, got)
		}
	}
}
