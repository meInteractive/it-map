package parser

import (
	"context"
	"encoding/json"
	"errors"

	"itmap/internal/aiproviders"
)

// ErrWorkerDrainBeforePaid означает, что StopClaims закрыт до начала нового
// Billing.Run. Он НЕ утверждает, что ранее начатый POST отменён или оплачен.
var ErrWorkerDrainBeforePaid = errors.New("worker draining before paid AI dispatch")

// workerStopBilling проверяет сигнал остановки до передачи запроса в durable
// резерв и провайдер. Уже начатый Billing.Run не прерывается: его settlement
// и сохранение AI-результата выполняются в рамках штатного grace context.
type workerStopBilling struct {
	delegate aiproviders.Billing
	stop     <-chan struct{}
}

func (b workerStopBilling) Run(ctx context.Context, request json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if claimsStopped(b.stop) {
		return nil, ErrWorkerDrainBeforePaid
	}
	return b.delegate.Run(ctx, request, work)
}
