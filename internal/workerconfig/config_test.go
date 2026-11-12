package workerconfig

import (
	"context"
	"errors"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseRequiresExplicitEnable(t *testing.T) {
	for _, enabled := range []string{"", "0", "true", " 1"} {
		if _, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": enabled, "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": "2000"})); !errors.Is(err, ErrDisabled) {
			t.Fatalf("enable=%q: %v", enabled, err)
		}
	}
	if _, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": "2000"})); !errors.Is(err, ErrDisabled) {
		t.Fatalf("parser execution gate was bypassed: %v", err)
	}
	for _, deadline := range []string{"", "999", "bad"} {
		cfg, err := Parse(env(map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_SCHEDULER_UNTIL": deadline}))
		if err != nil || cfg.IdleDelay <= 0 || cfg.ErrorMin <= 0 || cfg.ErrorMax < cfg.ErrorMin || cfg.RecoveryInterval <= 0 {
			t.Fatalf("legacy deadline=%q: invalid config %+v %v", deadline, cfg, err)
		}
	}
}

func TestContextHonoursParent(t *testing.T) {
	cfg := Config{}
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := cfg.Context(parent)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("unexpected worker deadline")
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
}
