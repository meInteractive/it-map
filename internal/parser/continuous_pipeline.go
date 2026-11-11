package parser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// TryPipelineSession выполняет ограниченную сессию только при наличии работы.
// Снимок не является claim: Cycle повторно проверяет владение и допуск.
// Планирование источников и восстановление истёкших leases вызываются отдельно.
func (c CycleControl) TryPipelineSession(ctx context.Context) (bool, error) {
	if c.DB == nil {
		return false, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return false, settingsFailure(ctx, ErrSettingsStorage)
	}
	runs := RunRepository{DB: c.DB, Generation: generation}
	source, allow := "hh", true
	options := ClaimOptions{Types: []string{"classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"}, SourceID: &source, AllowApify: &allow, VerifiedHHOnly: true}
	ready, err := runs.HasEligibleJob(ctx, options)
	if err != nil || !ready {
		return false, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return false, ErrRunStorage
	}
	w, err := c.worker(generation)
	if err != nil {
		return false, err
	}
	w.VerifiedHHOnly = true
	w.ContinueOnStageFailure = true
	// Ускоренный pipeline не должен менять частоту новых платных Actor.
	// Существующие операции по подтверждённому Run ID по-прежнему допускают GET.
	w.DenyActorStarts = true
	result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: "worker:" + hex.EncodeToString(token[:]), Trigger: "automatic"}, MaxJobs: 50, QueueProfile: "pipeline"})
	if err != nil {
		return false, err
	}
	return result.Processed > 0, nil
}
