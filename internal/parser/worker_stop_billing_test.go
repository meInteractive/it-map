package parser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/aiproviders"
)

// Сигнал остановки запрещает только новый платный вызов. Работа, уже
// переданная в Billing.Run, продолжает settlement в активном grace context.
type syntheticStopBilling struct {
	calls int
	inside func()
}

func (s *syntheticStopBilling) Run(ctx context.Context, _ json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	s.calls++
	if s.inside != nil {
		s.inside()
	}
	return work(ctx)
}

func TestGO431StopBillingGrace(t *testing.T) {
	payload := json.RawMessage(`{"mock":true}`)
	t.Run("stop_already_closed", func(t *testing.T) {
		stop := make(chan struct{})
		close(stop)
		base := &syntheticStopBilling{}
		calls := 0
		_, err := (workerStopBilling{delegate: base, stop: stop}).Run(context.Background(), payload, func(context.Context) (json.RawMessage, error) {
			calls++
			return payload, nil
		})
		if !errors.Is(err, ErrWorkerDrainBeforePaid) || base.calls != 0 || calls != 0 {
			t.Fatal("closed StopClaims permitted paid dispatch", err, base.calls, calls)
		}
	})
	t.Run("stop_after_dispatch", func(t *testing.T) {
		stop := make(chan struct{})
		base := &syntheticStopBilling{inside: func() { close(stop) }}
		billing := workerStopBilling{delegate: base, stop: stop}
		calls := 0
		out, err := billing.Run(context.Background(), payload, func(ctx context.Context) (json.RawMessage, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			calls++
			return payload, nil
		})
		if err != nil || string(out) != string(payload) || base.calls != 1 || calls != 1 {
			t.Fatal("in-flight settlement interrupted by StopClaims", err, base.calls, calls)
		}
		_, err = billing.Run(context.Background(), payload, func(context.Context) (json.RawMessage, error) {
			calls++
			return payload, nil
		})
		if !errors.Is(err, ErrWorkerDrainBeforePaid) || base.calls != 1 || calls != 1 {
			t.Fatal("new paid request started after shutdown", err, base.calls, calls)
		}
	})
	t.Run("nil_stop_preserves_manual", func(t *testing.T) {
		base := &syntheticStopBilling{}
		billing := workerStopBilling{delegate: base}
		_, err := billing.Run(context.Background(), payload, func(context.Context) (json.RawMessage, error) { return payload, nil })
		if err != nil || base.calls != 1 {
			t.Fatal("nil StopClaims blocked an independent manual stage", err, base.calls)
		}
	})
}

// Проверяем, что адаптер между Cycle и AIStageExecutor действительно
// передаёт сигнал, иначе защита Billing.Run на уровне executor не сработает.
func TestGO431AIStageDispatcherPropagatesStopClaims(t *testing.T) {
	stop := make(chan struct{})
	ex, err := (AIStageDispatcher{Owner: "CI_OWNER", StopClaims: stop}).executor("extract_vacancy")
	if err != nil || ex == nil || ex.StopClaims != stop {
		t.Fatal("AI dispatcher lost shutdown signal", err)
	}
	var _ aiproviders.Billing = workerStopBilling{}
}
