package apifyruns

import (
	"context"
	"time"

	"itmap/internal/integrations/apify"
)

// RecoveryReadProvider deliberately has no Actor start method. It reads an
// authenticated account and an already known run; it cannot initiate work.
type RecoveryReadProvider interface {
	Account(context.Context) (apify.BillingAccount, error)
	Run(context.Context, string) (apify.ActorRun, error)
}

// RecoveryReader is an explicit, read-only preparation step. Resolve must
// return a credential from trusted configuration, and factory must bind that
// credential to the provider. Neither credentials nor responses are returned.
// Callers must independently validate ledger ownership and recheck all data
// under locks before any write; a successful read is not permission to start.
type RecoveryReader struct {
	Resolve   func(context.Context, string) (string, error)
	NewClient func(string) (RecoveryReadProvider, error)
	Now       func() time.Time
}

// Verify reads only the specified existing run after checking the authenticated
// account. It never issues Actor POST or updates the stored operation.
func (r RecoveryReader) Verify(ctx context.Context, row Record, profile string) (Record, error) {
	if ctx == nil || r.Resolve == nil || (profile != "free" && profile != "starter") || validateStoredIdentity(row) != nil {
		return Record{}, ErrRecoveryEvidence
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	token, err := r.Resolve(ctx, profile)
	if ctx.Err() != nil {
		return Record{}, ctx.Err()
	}
	if err != nil || token == "" {
		return Record{}, ErrRecoveryEvidence
	}
	factory := r.NewClient
	if factory == nil {
		factory = func(token string) (RecoveryReadProvider, error) { return apify.New(token) }
	}
	client, err := factory(token)
	if ctx.Err() != nil {
		return Record{}, ctx.Err()
	}
	if err != nil || client == nil {
		return Record{}, ErrRecoveryEvidence
	}
	account, err := client.Account(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return Record{}, ctx.Err()
		}
		return Record{}, ErrRecoveryEvidence
	}
	if ctx.Err() != nil {
		return Record{}, ctx.Err()
	}
	if account.AccountID == "" || account.AccountID != row.AccountID {
		return Record{}, ErrRecoveryEvidence
	}
	run, err := client.Run(ctx, row.RunID)
	if err != nil {
		if ctx.Err() != nil {
			return Record{}, ctx.Err()
		}
		return Record{}, ErrRecoveryEvidence
	}
	if ctx.Err() != nil {
		return Record{}, ctx.Err()
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	result, err := VerifyRecoveryObservation(row, account.AccountID, run, now)
	if err != nil {
		return Record{}, ErrRecoveryEvidence
	}
	return result, nil
}
