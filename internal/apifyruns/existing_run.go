package apifyruns

import (
	"context"
	"errors"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

// ExistingRun checks only the durable operation journal; it never calls Apify.
// A missing run ID is deliberately not permission to start or retry an Actor.
func (r Repository) ExistingRun(ctx context.Context, v apifysettings.OperationVersion, key string, options apify.HHOptions) (bool, error) {
	scoped, err := operationKey(v, key, options)
	if err != nil {
		return false, err
	}
	row, err := r.readOperation(ctx, v, scoped)
	if errors.Is(err, errNoOperation) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.RunID != "" && (row.State == "running" || row.State == "succeeded" || row.State == "failed" || row.State == "uncertain"), nil
}
