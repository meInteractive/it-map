package consumercontrol

import (
	"context"
	"errors"
	"testing"
)

func TestRunDeadlineRequiresCurrentOwnerAndBoundedLease(t *testing.T) {
	token := testToken(1)
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	worker := State{Generation: testGeneration, Revision: 2, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 2000}
	req := Request{Generation: testGeneration, Revision: 2, Origin: WorkerRun, Token: token}
	for _, deadline := range []int64{1001, 1999, 2000} {
		if err := CanStartRun(worker, req, deadline, 1000); err != nil {
			t.Fatalf("deadline %d: %v", deadline, err)
		}
	}
	for _, deadline := range []int64{0, 1000, 2001} {
		if err := CanStartRun(worker, req, deadline, 1000); !errors.Is(err, ErrDenied) {
			t.Fatalf("deadline %d: %v", deadline, err)
		}
	}
	if err := CanStartRun(worker, req, 2000, 2000); !errors.Is(err, ErrDenied) {
		t.Fatal("expired owner accepted", err)
	}
	wrong := req
	wrong.Token = testToken(2)
	if err := CanStartRun(worker, wrong, 1500, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("wrong owner accepted", err)
	}
	wrong = req
	wrong.Revision++
	if err := CanStartRun(worker, wrong, 1500, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("stale owner accepted", err)
	}
	legacy := State{Generation: testGeneration, Revision: 1, Mode: Legacy}
	legacyReq := Request{Generation: testGeneration, Revision: 1, Origin: LegacyTick}
	if err := CanStartRun(legacy, legacyReq, 5000, 1000); err != nil {
		t.Fatal("legacy should not require worker lease", err)
	}
	if err := CanStartRun(legacy, legacyReq, 1000, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("expired run accepted", err)
	}
	if err := GuardRunLease(context.Background(), nil, req, 1500); !errors.Is(err, ErrState) {
		t.Fatal("nil transaction accepted", err)
	}
}
