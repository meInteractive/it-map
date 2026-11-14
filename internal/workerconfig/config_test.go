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

func TestRuntimeConfiguration(t *testing.T) {
	values := map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1"}
	cfg, err := Parse(env(values))
	if err != nil || cfg != Defaults() {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	values["ITMAP_WORKER_LAUNCH_IDLE_SECONDS"] = "15"
	values["ITMAP_WORKER_RECONCILE_IDLE_SECONDS"] = "7"
	values["ITMAP_WORKER_PIPELINE_IDLE_SECONDS"] = "2"
	values["ITMAP_WORKER_ERROR_MIN_SECONDS"] = "3"
	values["ITMAP_WORKER_ERROR_MAX_SECONDS"] = "90"
	values["ITMAP_WORKER_MAX_JOBS"] = "2"
	values["ITMAP_WORKER_SHUTDOWN_GRACE_SECONDS"] = "12"
	cfg, err = Parse(env(values))
	if err != nil || cfg.LaunchIdle != 15*time.Second || cfg.ReconcileIdle != 7*time.Second || cfg.IdleDelay != 2*time.Second || cfg.ErrorMin != 3*time.Second || cfg.ErrorMax != 90*time.Second || cfg.MaxJobs != 2 || cfg.ShutdownGrace != 12*time.Second {
		t.Fatalf("custom: %+v %v", cfg, err)
	}
}

func TestRuntimeConfigurationRejectsUnsafeValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ITMAP_WORKER_LAUNCH_IDLE_SECONDS", "14"}, {"ITMAP_WORKER_LAUNCH_IDLE_SECONDS", "61"},
		{"ITMAP_WORKER_RECONCILE_IDLE_SECONDS", "4"}, {"ITMAP_WORKER_PIPELINE_IDLE_SECONDS", "0"},
		{"ITMAP_WORKER_ERROR_MIN_SECONDS", "61"}, {"ITMAP_WORKER_ERROR_MAX_SECONDS", "121"},
		{"ITMAP_WORKER_MAX_JOBS", "51"}, {"ITMAP_WORKER_MAX_JOBS", "-1"},
		{"ITMAP_WORKER_MAX_JOBS", " 2"}, {"ITMAP_WORKER_MAX_JOBS", "1.5"},
		{"ITMAP_WORKER_MAX_JOBS", "99999999999999999999999999999999999"},
		{"ITMAP_WORKER_SHUTDOWN_GRACE_SECONDS", "31"}, {"ITMAP_WORKER_SHUTDOWN_GRACE_SECONDS", "-1"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			values := map[string]string{"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", tc.key: tc.value}
			_, err := Parse(env(values))
			if !errors.Is(err, ErrConfig) || err.Error() != ErrConfig.Error() {
				t.Fatalf("unsafe configuration accepted or exposed: %v", err)
			}
		})
	}
}
