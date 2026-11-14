package workerengine

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/parser"
	"itmap/internal/workerconfig"
	"itmap/internal/workerloop"
)

func TestRunRequiresMatchingDatabaseAndHonorsCancellation(t *testing.T) {
	if err := (Engine{}).Run(context.Background()); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatal("worker без базы запущен", err)
	}
	db := &sql.DB{}
	worker := parser.Worker{Runs: parser.RunRepository{DB: db}, Settings: apifysettings.Repository{DB: &sql.DB{}}}
	if err := (Engine{Worker: worker}).Run(context.Background()); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatal("разные базы приняты", err)
	}
	worker.Settings.DB = db
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := (Engine{Worker: worker, OnResult: func(workerloop.ProfileName, workerloop.ProfileOutcome) { called = true }}).Run(ctx)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("отменённый worker выполнил шаг", err)
	}
}

func TestProfilesUseValidatedRuntimeConfiguration(t *testing.T) {
	cfg := workerconfig.Defaults()
	cfg.LaunchIdle = 17 * time.Second
	cfg.ReconcileIdle = 9 * time.Second
	cfg.IdleDelay = 2 * time.Second
	cfg.ErrorMin = 3 * time.Second
	cfg.ErrorMax = 90 * time.Second
	profiles := (Engine{}).profiles(cfg)
	for i, idle := range []time.Duration{cfg.LaunchIdle, cfg.ReconcileIdle, cfg.IdleDelay} {
		p := profiles[i]
		if p.IdleDelay != idle || p.ErrorMin != cfg.ErrorMin || p.ErrorMax != cfg.ErrorMax || p.StepTimeout != 2*time.Minute {
			t.Fatalf("profile %s has wrong bounds: %+v", p.Name, p)
		}
	}
	if err := workerloop.ValidateProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	db := &sql.DB{}
	w := parser.Worker{Runs: parser.RunRepository{DB: db}, Settings: apifysettings.Repository{DB: db}}
	cfg.MaxJobs = 51
	if err := (Engine{Worker: w, Config: &cfg}).Run(context.Background()); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatal("invalid config reached worker", err)
	}
}
