package workerengine

import (
	"context"
	"database/sql"

	"itmap/internal/paidrecovery"
)

// RecoverySnapshot — только категории сохранённых записей, не право запуска.
type RecoverySnapshot struct {
	ActorCurrentUnresolved          int `json:"actorCurrentUnresolved"`
	CurrentAIReservedRunning        int `json:"currentAIReservedRunning"`
	CurrentAIReservedOther          int `json:"currentAIReservedOther"`
	ActorUnknown                    int `json:"actorUnknown"`
	ActorReconcile                  int `json:"actorReconcile"`
	ActorCostRecorded               int `json:"actorCostRecorded"`
	SchedulerHistoricalUnknown      int `json:"schedulerHistoricalUnknown"`
	SchedulerHistoricalCostRecorded int `json:"schedulerHistoricalCostRecorded"`
	SchedulerOrphanUnknown          int `json:"schedulerOrphanUnknown"`
	SchedulerOrphanCostRecorded     int `json:"schedulerOrphanCostRecorded"`
}
type RecoveryReader interface {
	ReadRecovery(context.Context) (RecoverySnapshot, error)
}
type sqlRecoveryReader struct {
	db         *sql.DB
	generation string
}

func NewRecoveryReader(db *sql.DB, generation string) RecoveryReader {
	return &sqlRecoveryReader{db: db, generation: generation}
}
func (r *sqlRecoveryReader) ReadRecovery(ctx context.Context) (RecoverySnapshot, error) {
	if r == nil || r.db == nil || ctx == nil || r.generation == "" {
		return RecoverySnapshot{}, errQueue
	}
	snap, err := paidrecovery.Inspect(ctx, r.db, r.generation)
	if err != nil {
		return RecoverySnapshot{}, errQueue
	}
	return RecoverySnapshot{CurrentAIReservedRunning: snap.CurrentAIReservedRunning, CurrentAIReservedOther: snap.CurrentAIReservedOther, ActorCurrentUnresolved: snap.CurrentActorUnresolved, ActorUnknown: snap.Apify.Unknown, ActorReconcile: snap.Apify.Reconcile, ActorCostRecorded: snap.Apify.TerminalCostRecorded,
		SchedulerHistoricalUnknown: snap.Scheduler.HistoricalUnknown, SchedulerHistoricalCostRecorded: snap.Scheduler.HistoricalCostRecorded, SchedulerOrphanUnknown: snap.Scheduler.OrphanUnknown, SchedulerOrphanCostRecorded: snap.Scheduler.OrphanCostRecorded}, nil
}
