package consumercontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

const testGeneration = "11111111-2222-4333-8444-555555555555"

func testToken(seed byte) Token {
	var token Token
	for i := range token.value {
		token.value[i] = seed
	}
	return token
}

func TestGuardAndDigestRejectInvalidInput(t *testing.T) {
	if err := GuardNewRun(context.Background(), nil, Request{}); !errors.Is(err, ErrState) {
		t.Fatalf("nil transaction accepted: %v", err)
	}
	if _, err := (Token{}).Digest(); !errors.Is(err, ErrState) {
		t.Fatalf("zero token accepted: %v", err)
	}
	if _, err := BeginDrain(context.Background(), nil, testGeneration, 1); !errors.Is(err, ErrState) {
		t.Fatalf("nil drain transaction accepted: %v", err)
	}
}

func TestConsumerTransitionsRequireQuiescenceAndEvidence(t *testing.T) {
	legacy := State{Generation: testGeneration, Revision: 1, Mode: Legacy}
	draining, err := Transition(legacy, Change{Generation: testGeneration, Revision: 1, To: Draining}, 1000)
	if err != nil || draining.Mode != Draining || draining.Revision != 2 {
		t.Fatalf("drain: %+v %v", draining, err)
	}
	for _, tc := range []Change{
		{Generation: testGeneration, Revision: 2, To: Worker, Owner: testToken(1), LeaseUntilMS: 2000},
		{Generation: testGeneration, Revision: 2, To: Worker, Owner: testToken(1), LeaseUntilMS: 2000, Quiescent: true},
		{Generation: testGeneration, Revision: 2, To: Worker, Owner: testToken(1), LeaseUntilMS: 1000, Quiescent: true, EvidenceVerified: true},
		{Generation: testGeneration, Revision: 2, To: Worker, LeaseUntilMS: 2000, Quiescent: true, EvidenceVerified: true},
		{Generation: testGeneration, Revision: 2, To: Worker, Owner: testToken(1), LeaseUntilMS: 1001 + MaxOwnerLeaseMS, Quiescent: true, EvidenceVerified: true},
	} {
		if _, err := Transition(draining, tc, 1000); !errors.Is(err, ErrTransition) {
			t.Fatalf("unsafe worker transition: %v", err)
		}
	}
	worker, err := Transition(draining, Change{Generation: testGeneration, Revision: 2, To: Worker, Owner: testToken(1), LeaseUntilMS: 2000, Quiescent: true, EvidenceVerified: true}, 1000)
	if err != nil || worker.Mode != Worker || worker.Revision != 3 {
		t.Fatalf("worker: %+v %v", worker, err)
	}
	if _, err := Transition(worker, Change{Generation: testGeneration, Revision: 3, To: Legacy, Quiescent: true, EvidenceVerified: true}, 1000); !errors.Is(err, ErrTransition) {
		t.Fatal("direct rollback allowed")
	}
	drained, err := Transition(worker, Change{Generation: testGeneration, Revision: 3, To: Draining}, 1000)
	if err != nil || drained.OwnerDigest != worker.OwnerDigest {
		t.Fatal("drain lost worker fence", err)
	}
	if _, err := Transition(drained, Change{Generation: testGeneration, Revision: 4, To: Worker, Owner: testToken(1), LeaseUntilMS: 2000, Quiescent: true, EvidenceVerified: true}, 1000); !errors.Is(err, ErrTransition) {
		t.Fatal("previous worker credential reused")
	}
	if _, err := Transition(drained, Change{Generation: testGeneration, Revision: 4, To: Legacy, EvidenceVerified: true}, 1000); !errors.Is(err, ErrTransition) {
		t.Fatal("rollback allowed active work")
	}
	restored, err := Transition(drained, Change{Generation: testGeneration, Revision: 4, To: Legacy, Quiescent: true, EvidenceVerified: true}, 1000)
	if err != nil || restored.Mode != Legacy || restored.OwnerDigest != ([32]byte{}) || restored.LeaseUntilMS != 0 {
		t.Fatal("legacy reset failed", err)
	}
}

func TestConsumerStartFailsClosedOnStaleOrWrongOwner(t *testing.T) {
	token := testToken(1)
	digest, err := token.digest()
	if err != nil {
		t.Fatal(err)
	}
	worker := State{Generation: testGeneration, Revision: 9, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: 2000}
	valid := Request{Generation: testGeneration, Revision: 9, Origin: WorkerRun, Token: token}
	if err := CanStart(worker, valid, 1999); err != nil {
		t.Fatal(err)
	}
	if err := CanStart(worker, valid, 2000); !errors.Is(err, ErrDenied) {
		t.Fatal("expired lease accepted")
	}
	corrupt := worker
	corrupt.LeaseUntilMS = 1001 + MaxOwnerLeaseMS
	if err := CanStart(corrupt, valid, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("unbounded stored lease accepted")
	}
	wrong := valid
	wrong.Token = testToken(2)
	if err := CanStart(worker, wrong, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("wrong owner accepted")
	}
	stale := valid
	stale.Revision--
	if err := CanStart(worker, stale, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("stale revision accepted")
	}
	stale = valid
	stale.Generation = "11111111-2222-4333-8444-555555555556"
	if err := CanStart(worker, stale, 1000); !errors.Is(err, ErrStale) {
		t.Fatal("stale generation accepted")
	}
	for _, origin := range []Origin{LegacyTick, Manual, Diagnostic, "unknown"} {
		req := valid
		req.Origin = origin
		if err := CanStart(worker, req, 1000); !errors.Is(err, ErrDenied) {
			t.Fatalf("origin %q accepted", origin)
		}
	}
	draining := worker
	draining.Mode = Draining
	if err := CanStart(draining, valid, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("draining accepted new work")
	}
	invalid := worker
	invalid.Revision = uint64(math.MaxInt64) + 1
	if err := CanStart(invalid, valid, 1000); !errors.Is(err, ErrState) {
		t.Fatal("invalid revision accepted")
	}
	invalid = State{Generation: testGeneration, Revision: 1, Mode: Draining, LeaseUntilMS: -1}
	if _, err := Transition(invalid, Change{Generation: testGeneration, Revision: 1, To: Legacy, Quiescent: true, EvidenceVerified: true}, 1000); !errors.Is(err, ErrState) {
		t.Fatal("negative draining lease accepted")
	}
}

func TestLegacyStartAndTokenRedaction(t *testing.T) {
	state := State{Generation: testGeneration, Revision: 1, Mode: Legacy}
	for _, origin := range []Origin{LegacyTick, Manual, Diagnostic} {
		if err := CanStart(state, Request{Generation: testGeneration, Revision: 1, Origin: origin}, 1000); err != nil {
			t.Fatalf("legacy %q: %v", origin, err)
		}
	}
	if err := CanStart(state, Request{Generation: testGeneration, Revision: 1, Origin: WorkerRun}, 1000); !errors.Is(err, ErrDenied) {
		t.Fatal("worker allowed in legacy")
	}
	token := testToken('A')
	encoded, err := json.Marshal(token)
	var decoded string
	if err != nil || json.Unmarshal(encoded, &decoded) != nil || decoded != "<redacted>" || strings.Contains(fmt.Sprintf("%v %#v", token, token), "AAAA") {
		t.Fatal("token not redacted")
	}
}

func TestMalformedLeaseAndRevisionFailClosed(t *testing.T) {
	digest, err := testToken(1).digest()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []State{
		{Generation: testGeneration, Revision: 1, Mode: Legacy, LeaseUntilMS: -1},
		{Generation: testGeneration, Revision: 1, Mode: Draining, LeaseUntilMS: -1},
		{Generation: testGeneration, Revision: 1, Mode: Worker, OwnerDigest: digest, LeaseUntilMS: -1},
	} {
		if err := CanStart(state, Request{Generation: testGeneration, Revision: 1}, 1000); !errors.Is(err, ErrState) {
			t.Fatalf("negative lease accepted for %s: %v", state.Mode, err)
		}
	}
	state := State{Generation: testGeneration, Revision: math.MaxInt64, Mode: Legacy}
	if _, err := Transition(state, Change{Generation: testGeneration, Revision: math.MaxInt64, To: Draining}, 1000); !errors.Is(err, ErrState) {
		t.Fatal("revision overflow accepted")
	}
}
