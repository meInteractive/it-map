package workerengine

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/parser"
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
