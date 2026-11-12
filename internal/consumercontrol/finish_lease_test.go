package consumercontrol

import (
	"context"
	"errors"
	"testing"
)

func TestCompletionLeasesMustBeLiveAndNested(t *testing.T) {
	token := testToken(1)
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	worker := State{Generation: testGeneration, Revision: 3, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 3000}
	req := Request{Generation: testGeneration, Revision: 3, Origin: WorkerRun, Token: token}
	draining, err := Transition(worker, Change{Generation: testGeneration, Revision: 3, To: Draining}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []State{worker, draining} {
		for _, tc := range []struct {
			run, job, now int64
			want          error
		}{
			{3000, 3000, 1000, nil}, {2500, 2000, 1000, nil},
			{3001, 2000, 1000, ErrDenied}, {2500, 2501, 1000, ErrDenied},
			{2500, 1000, 1000, ErrDenied}, {1000, 1000, 1000, ErrDenied}, {3000, 2000, 3000, ErrDenied},
		} {
			if err := CanFinishLeases(state, req, tc.run, tc.job, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("mode=%s run=%d job=%d: %v", state.Mode, tc.run, tc.job, err)
			}
		}
	}
	if err := GuardCompletionLeases(context.Background(), nil, req, 2500, 2000); !errors.Is(err, ErrState) {
		t.Fatal("nil transaction", err)
	}
}
