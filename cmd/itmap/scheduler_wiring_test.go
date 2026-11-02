package main

import (
	"database/sql"
	"testing"

	"itmap/internal/httpapi"
)

func TestSchedulerExecutionWiringRequiresExplicitFlag(t *testing.T) {
	db := &sql.DB{}
	t.Setenv("ITMAP_PARSER_EXECUTION_ENABLED", "")
	t.Setenv("SCHEDULER_TOKEN", "CI_TICK_TOKEN")
	opts := httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle != nil || opts.SchedulerTick != nil {
		t.Fatal("scheduler enabled by default")
	}
	t.Setenv("ITMAP_PARSER_EXECUTION_ENABLED", "1")
	t.Setenv("SCHEDULER_TOKEN", "")
	opts = httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle == nil || opts.SchedulerTick != nil {
		t.Fatal("manual/tick gating")
	}
	t.Setenv("SCHEDULER_TOKEN", "CI_TICK_TOKEN")
	opts = httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle == nil || opts.SchedulerTick == nil || opts.SchedulerToken != "CI_TICK_TOKEN" {
		t.Fatal("configured scheduler not wired")
	}
}
