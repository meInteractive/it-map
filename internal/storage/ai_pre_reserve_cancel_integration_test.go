//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"itmap/internal/parser"
)

// GO-431 P0: cancellation known before reserve must not manufacture evidence
// that a paid provider request might have been sent. This is disposable CI
// MariaDB, not production and not a real external provider call.
func TestMariaDBAIAlreadyCanceledBeforeReserveDisposable(t *testing.T) {
	f := billingFixture(t)
	for _, tc := range []struct {
		name        string
		makeContext func(context.Context) (context.Context, context.CancelFunc)
		want        error
	}{
		{"cancelled", func(parent context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			cancel()
			return ctx, cancel
		}, context.Canceled},
		{"deadline_exceeded", func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithDeadline(parent, time.Now().Add(-time.Second))
		}, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := f.sub(t)
			billingReset(f)
			run, _, attempt := billingStart(t, f, "CI_AI_PRE_RESERVE_"+tc.name)
			cancelled, cancel := tc.makeContext(f.ctx)
			defer cancel()
			sent := 0
			_, err := attempt.Run(cancelled, syntheticPaid, func(context.Context) (json.RawMessage, error) {
				sent++
				return syntheticUsage, nil
			})
			if !errors.Is(err, tc.want) || sent != 0 || attempt.Requests() != 0 || attempt.Unknown() {
				t.Fatal("known pre-reserve cancellation became an unknown paid operation", err, sent, attempt.Requests(), attempt.Unknown())
			}
			reserve, cost, status := billingAmount(t, f, attempt.ID())
			if reserve != 0 || cost.Valid || status != "running" ||
				f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id=? AND COALESCE(provider,'')='' AND COALESCE(model,'')=''", attempt.ID()) != 1 {
				t.Fatal("pre-reserve cancellation wrote provider identity or reserved money", reserve, cost, status)
			}
			// Only because cancellation preceded all reserve/POST work may the
			// same still-owned handle subsequently call a synthetic provider.
			_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
				sent++
				return syntheticUsage, nil
			})
			if err != nil || sent != 1 || attempt.Requests() != 1 || attempt.Unknown() {
				t.Fatal("provably unsent cancelled call prevented safe later execution", err, sent)
			}
			if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "completed"}}); err != nil {
				t.Fatal("cannot settle known synthetic payment", err)
			}
			reserve, cost, status = billingAmount(t, f, attempt.ID())
			if status != "completed" || !cost.Valid || math.Abs(reserve-0.01) > 1e-9 || math.Abs(cost.Float64-0.01) > 1e-9 {
				t.Fatal("known successful retry was not durably settled", status, reserve, cost)
			}
			f.finish(run)
		})
	}
}
