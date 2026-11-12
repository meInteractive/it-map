package consumercontrol

import (
	"context"
	"errors"
	"testing"
)

func TestClaimLeaseRequiresNestedLiveDeadlines(t *testing.T) {
	token := testToken(1)
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	s := State{Generation: testGeneration, Revision: 2, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 3000}
	r := Request{Generation: testGeneration, Revision: 2, Origin: WorkerRun, Token: token}
	for _, tc := range []struct {
		run, job, now int64
		want          error
	}{
		{2500, 2500, 1000, nil}, {3000, 3000, 1000, nil}, {2500, 1001, 1000, nil},
		{2500, 2501, 1000, ErrDenied}, {2500, 1000, 1000, ErrDenied},
		{3001, 2500, 1000, ErrDenied}, {1000, 1000, 1000, ErrDenied}, {3000, 2500, 3000, ErrDenied},
	} {
		if err := CanClaimJob(s, r, tc.run, tc.job, tc.now); !errors.Is(err, tc.want) {
			t.Fatalf("run=%d job=%d now=%d: %v", tc.run, tc.job, tc.now, err)
		}
	}
	wrong := r
	wrong.Token = testToken(2)
	if err := CanClaimJob(s, wrong, 2500, 2000, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("wrong owner", err)
	}
	wrong = r
	wrong.Revision++
	if err := CanClaimJob(s, wrong, 2500, 2000, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("stale revision", err)
	}
	if err := GuardClaimLease(context.Background(), nil, r, 2500, 2000); !errors.Is(err, ErrState) {
		t.Fatal("nil transaction", err)
	}
}
