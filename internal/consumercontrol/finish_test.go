package consumercontrol

import (
	"errors"
	"testing"
)

func TestFinishRequiresOriginalLiveWorkerFence(t *testing.T) {
	token := testToken(1)
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	worker := State{Generation: testGeneration, Revision: 7, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 2000}
	req := Request{Generation: testGeneration, Revision: 7, Origin: WorkerRun, Token: token}
	if err := CanFinish(worker, req, 1000); err != nil {
		t.Fatal(err)
	}
	draining, err := Transition(worker, Change{Generation: testGeneration, Revision: 7, To: Draining}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := CanFinish(draining, req, 1000); err != nil {
		t.Fatal("existing work cannot finish", err)
	}
	current := req
	current.Revision = draining.Revision
	if err := CanStart(draining, current, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("draining allowed start", err)
	}
	if err := CanFinish(draining, current, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("new draining revision accepted", err)
	}
	for _, tc := range []struct {
		name  string
		state State
		req   Request
		now   int64
		want  error
	}{
		{"expired", draining, req, 2000, ErrDenied},
		{"old revision", draining, Request{Generation: testGeneration, Revision: 6, Origin: WorkerRun, Token: token}, 1000, ErrStale},
		{"other generation", draining, Request{Generation: "00000000-0000-0000-0000-000000000000", Revision: 7, Origin: WorkerRun, Token: token}, 1000, ErrStale},
		{"wrong owner", draining, Request{Generation: testGeneration, Revision: 7, Origin: WorkerRun, Token: testToken(2)}, 1000, ErrDenied},
		{"manual", draining, Request{Generation: testGeneration, Revision: 7, Origin: Manual, Token: token}, 1000, ErrDenied},
		{"legacy", State{Generation: testGeneration, Revision: 8, Mode: Legacy}, req, 1000, ErrDenied},
		{"invalid clock", worker, req, 0, ErrState},
		{"too long", State{Generation: testGeneration, Revision: 7, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 1001 + MaxOwnerLeaseMS}, req, 1000, ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CanFinish(tc.state, tc.req, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	newToken := testToken(2)
	next, err := Transition(draining, Change{Generation: testGeneration, Revision: 8, To: Worker, Owner: newToken, LeaseUntilMS: 2000, Quiescent: true, EvidenceVerified: true}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := CanFinish(next, req, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("old worker survived reassignment", err)
	}
}
