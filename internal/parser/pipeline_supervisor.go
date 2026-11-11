package parser

import (
	"context"
	"time"

	"itmap/internal/workerloop"
)

// PipelineRuntime задаёт ограниченные операции одного потребителя.
type PipelineRuntime interface {
	RecoverPipeline(context.Context) error
	TryPipelineSession(context.Context) (bool, error)
}

// RecoverPipeline повторно читает поколение перед восстановлением leases.
func (c CycleControl) RecoverPipeline(ctx context.Context) error {
	if c.DB == nil {
		return ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return settingsFailure(ctx, ErrSettingsStorage)
	}
	return (RunRepository{DB: c.DB, Generation: generation}).RecoverExpired(ctx)
}

// RunPipelineSupervisor обрабатывает только pipeline; запуск и планирование HH
// остаются отдельными. Вызов блокируется до отмены контекста и сам не стартует.
func RunPipelineSupervisor(ctx context.Context, runtime PipelineRuntime, recoveryInterval time.Duration, cfg workerloop.Config) error {
	if runtime == nil || recoveryInterval <= 0 {
		return workerloop.ErrConfig
	}
	var nextRecovery time.Time
	return workerloop.Run(ctx, cfg, func(ctx context.Context) (bool, error) {
		now := time.Now()
		if nextRecovery.IsZero() || !now.Before(nextRecovery) {
			if err := runtime.RecoverPipeline(ctx); err != nil {
				return false, err
			}
			nextRecovery = time.Now().Add(recoveryInterval)
		}
		return runtime.TryPipelineSession(ctx)
	})
}
