package main

import (
	"database/sql"
	"testing"

	"itmap/internal/httpapi"
	"itmap/internal/parser"
)

func TestSchedulerExecutionWiringRequiresExplicitFlag(t *testing.T) {
	db := &sql.DB{}
	t.Setenv("ITMAP_PARSER_EXECUTION_ENABLED", "")
	t.Setenv("SCHEDULER_TOKEN", "CI_TICK_TOKEN")
	opts := httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle != nil || opts.SchedulerTick != nil || opts.SchedulerProbe != nil {
		t.Fatal("scheduler enabled by default")
	}
	t.Setenv("ITMAP_PARSER_EXECUTION_ENABLED", "1")
	t.Setenv("SCHEDULER_TOKEN", "")
	opts = httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle == nil || opts.SchedulerTick != nil || opts.SchedulerProbe == nil {
		t.Fatal("manual/tick gating")
	}

	if opts.SchedulerCycle.(parser.CycleControl).TrustFreeProviderLimit {
		t.Fatal("provider limit fallback enabled by default")
	}
	t.Setenv("ITMAP_APIFY_TRUST_FREE_HARD_LIMIT", "1")
	capped := httpapi.Options{}
	configureScheduler(&capped, db)
	if !capped.SchedulerCycle.(parser.CycleControl).TrustFreeProviderLimit {
		t.Fatal("explicit provider limit fallback not wired")
	}
	t.Setenv("SCHEDULER_TOKEN", "CI_TICK_TOKEN")
	opts = httpapi.Options{}
	configureScheduler(&opts, db)
	if opts.SchedulerCycle == nil || opts.SchedulerTick == nil || opts.SchedulerToken != "CI_TICK_TOKEN" {
		t.Fatal("configured scheduler not wired")
	}
}
