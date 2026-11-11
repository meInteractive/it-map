package workerconfig

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseRequiresExplicitEnableAndValidPilot(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, enabled := range []string{"", "0", "true", " 1"} {
		if _, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": enabled, "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": "2000"}), now); !errors.Is(err, ErrDisabled) {
			t.Fatalf("enable=%q: %v", enabled, err)
		}
	}
	if _, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": "2000"}), now); !errors.Is(err, ErrDisabled) {
		t.Fatalf("parser execution gate was bypassed: %v", err)
	}
	for _, deadline := range []string{"", "1000", "999", "-2", "+2000", " 2000", "２０００", "1234567890123"} {
		if _, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": deadline}), now); !errors.Is(err, ErrDeadline) {
			t.Fatalf("deadline=%q: %v", deadline, err)
		}
	}
	cfg, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": "2000"}), now)
	if err != nil || cfg.Until.Unix() != 2000 || cfg.IdleDelay <= 0 || cfg.ErrorMin <= 0 || cfg.ErrorMax < cfg.ErrorMin || cfg.RecoveryInterval <= 0 {
		t.Fatalf("invalid accepted config: %+v %v", cfg, err)
	}
}

func TestContextHonoursPilotAndEarlierParent(t *testing.T) {
	now := time.Now()
	cfg, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": strconv.FormatInt(now.Unix()+60, 10)}), now)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancelParent := context.WithDeadline(context.Background(), now.Add(time.Second))
	defer cancelParent()
	ctx, cancel := cfg.Context(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(now.Add(time.Second)) {
		t.Fatal("parent deadline not honoured")
	}
	cancelParent()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not propagate")
	}
	pilotCtx, stop := cfg.Context(context.Background())
	defer stop()
	if got, ok := pilotCtx.Deadline(); !ok || !got.Equal(cfg.Until) {
		t.Fatal("pilot deadline not applied")
	}
}

func TestExpiredPilotContextCancelsInFlightWork(t *testing.T) {
	cfg := Config{Until: time.Now().Add(-time.Second)}
	ctx, cancel := cfg.Context(context.Background())
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("expired pilot did not cancel work")
	}
}
