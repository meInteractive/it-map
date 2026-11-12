package consumercontrol

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestRenewPreservesFenceAndRequiresLiveOwner(t *testing.T) {
	token := testToken(1)
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := State{Generation: testGeneration, Revision: 7, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 2000}
	req := Request{Generation: testGeneration, Revision: 7, Origin: WorkerRun, Token: token}
	next, err := Renew(state, req, 1000+MaxOwnerLeaseMS, 1000)
	if err != nil || next.LeaseUntilMS != 1000+MaxOwnerLeaseMS {
		t.Fatal("bounded renewal failed", err)
	}
	want := state
	want.LeaseUntilMS = next.LeaseUntilMS
	if next != want || state.LeaseUntilMS != 2000 {
		t.Fatal("renewal changed identity or input")
	}
	if err := CanStart(next, req, 2001); err != nil {
		t.Fatal("unchanged request lost renewed fence", err)
	}
	for _, tc := range []struct {
		name       string
		state      State
		req        Request
		until, now int64
		err        error
	}{
		{"expired", state, req, 3000, 2000, ErrDenied},
		{"same deadline", state, req, 2000, 1000, ErrDenied},
		{"shortening", state, req, 1999, 1000, ErrDenied},
		{"too long", state, req, 1001 + MaxOwnerLeaseMS, 1000, ErrDenied},
		{"overflow boundary", state, req, math.MaxInt64, 1000, ErrDenied},
		{"invalid clock", state, req, 3000, 0, ErrState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Renew(tc.state, tc.req, tc.until, tc.now); !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want %v", err, tc.err)
			}
		})
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Token = testToken(2) },
		func(r *Request) { r.Token = Token{} },
		func(r *Request) { r.Origin = Manual },
		func(r *Request) { r.Revision++ },
		func(r *Request) { r.Generation = "00000000-0000-0000-0000-000000000000" },
	} {
		wrong := req
		mutate(&wrong)
		if _, err := Renew(state, wrong, 3000, 1000); err == nil {
			t.Fatal("wrong owner or fence renewed")
		}
	}
	drained, err := Transition(state, Change{Generation: testGeneration, Revision: 7, To: Draining}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	req.Revision = drained.Revision
	if _, err := Renew(drained, req, 3000, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("draining renewed", err)
	}
	legacy := State{Generation: testGeneration, Revision: 7, Mode: Legacy}
	req.Revision, req.Origin = 7, LegacyTick
	if _, err := Renew(legacy, req, 3000, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("legacy renewed", err)
	}
	if _, err := RenewOwner(context.Background(), nil, req, 3000); !errors.Is(err, ErrState) {
		t.Fatal("nil transaction accepted", err)
	}
}
