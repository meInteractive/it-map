package workerengine

import (
	"context"
	"database/sql"
	"encoding/json"

	"itmap/internal/parser"
)

// HHScheduleSnapshot contains only aggregate schedule and pending-scan diagnostics.
// It is an observation, not permission to launch an Actor.
type HHScheduleSnapshot struct {
	Enabled          bool                         `json:"enabled"`
	SourceSelected   bool                         `json:"sourceSelected"`
	FavoritesOnly    bool                         `json:"favoritesOnly"`
	Summary          parser.HHScanScheduleSummary `json:"summary"`
	PendingHHScans   int64                        `json:"pendingHHScans"`
	RunningHHScans   int64                        `json:"runningHHScans"`
	PendingBreakdown parser.HHPendingBreakdown    `json:"pendingBreakdown"`
}
type HHScheduleReader interface {
	ReadHHSchedule(context.Context) (HHScheduleSnapshot, error)
}
type sqlHHScheduleReader struct {
	db         *sql.DB
	generation string
}

func NewHHScheduleReader(db *sql.DB, generation string) HHScheduleReader {
	return &sqlHHScheduleReader{db: db, generation: generation}
}
func (r *sqlHHScheduleReader) ReadHHSchedule(ctx context.Context) (HHScheduleSnapshot, error) {
	var result HHScheduleSnapshot
	if r == nil || r.db == nil || ctx == nil || r.generation == "" {
		return result, errQueue
	}
	current := func() bool {
		var g string
		return r.db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&g) == nil && g == r.generation
	}
	if !current() {
		return result, errQueue
	}
	raw, err := (parser.HHScanScheduleRepository{DB: r.db}).ReadHHScanSchedule(ctx)
	if err != nil || json.Unmarshal(raw, &result) != nil || !current() {
		return HHScheduleSnapshot{}, errQueue
	}
	return result, nil
}
