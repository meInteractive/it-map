package parser

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

type unguardedListingRuntime struct{}

func (unguardedListingRuntime) Execute(context.Context, apifysettings.OperationVersion, string, apify.HHOptions) (apifyruns.Record, error) {
	panic("must not execute")
}
func (unguardedListingRuntime) ReadDataset(context.Context, apifysettings.OperationVersion, string, apify.HHOptions, int64) (apifyruns.DatasetSlice, error) {
	panic("must not execute")
}

func TestWorkerCooldownRejectsUnprotectedRuntimeBeforeDatabase(t *testing.T) {
	db := &sql.DB{}
	w := Worker{Runs: RunRepository{DB: db}, Settings: apifysettings.Repository{DB: db}, ListingRuntime: unguardedListingRuntime{}, EnforceActorCooldown: true}
	if _, err := w.Cycle(context.Background(), WorkerOptions{MaxJobs: 1}); !errors.Is(err, ErrRunInput) {
		t.Fatalf("unguarded runtime accepted: %v", err)
	}
}
