package workerengine

import (
	"context"
	"errors"
	"testing"
)

func TestPauseHoldsAdmissionUntilActiveProfileFinishes(t *testing.T) {
	gate := NewPauseGate()
	ctx := context.Background()
	claims, err := gate.enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := gate.Pause("release"); err != nil || state != PauseDraining {
		t.Fatalf("pause = %s, %v", state, err)
	}
	select {
	case <-claims.done:
	default:
		t.Fatal("claims still open")
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	gate.leave(true)
	if state, err := gate.Pause("release"); err != nil || state != PauseHeld {
		t.Fatalf("pause = %s, %v", state, err)
	}
	blocked, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := gate.enter(blocked); !errors.Is(err, context.Canceled) {
		t.Fatalf("enter = %v", err)
	}
	if err := gate.Resume("another-release"); !errors.Is(err, ErrPauseConflict) {
		t.Fatalf("resume with wrong token = %v", err)
	}
	if err := gate.Resume("release"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.enter(ctx); err != nil {
		t.Fatal(err)
	}
	gate.leave(true)
}

func TestPauseDoesNotAcknowledgeFailedProfile(t *testing.T) {
	gate := NewPauseGate()
	for _, safe := range []bool{false, true} {
		if _, err := gate.enter(context.Background()); err != nil {
			t.Fatal(err)
		}
		gate.leave(safe)
	}
	if state, err := gate.Pause("release"); err != nil || state != PauseUnsafe {
		t.Fatalf("pause = %s, %v", state, err)
	}
}

func TestPauseRacesWithProfileAdmission(t *testing.T) {
	for range 100 {
		gate := NewPauseGate()
		ctx, cancel := context.WithCancel(context.Background())
		type result struct {
			signal *claimSignal
			err    error
		}
		entered := make(chan result, 1)
		go func() {
			signal, err := gate.enter(ctx)
			entered <- result{signal, err}
		}()

		state, err := gate.Pause("release")
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		entry := <-entered
		switch state {
		case PauseHeld:
			if !errors.Is(entry.err, context.Canceled) {
				t.Fatalf("admission during held pause = %v", entry.err)
			}
		case PauseDraining:
			if entry.err != nil {
				t.Fatal(entry.err)
			}
			select {
			case <-entry.signal.done:
			default:
				t.Fatal("admitted profile can still claim")
			}
			gate.leave(true)
		default:
			t.Fatalf("unexpected pause state %q", state)
		}
	}
}
