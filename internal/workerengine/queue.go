package workerengine

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var errQueue = errors.New("queue snapshot unavailable")

// QueueGroup — только агрегаты; due не является подтверждением допуска к claim.
type QueueGroup struct {
	Due              int64 `json:"due"`
	Delayed          int64 `json:"delayed"`
	Running          int64 `json:"running"`
	Failed           int64 `json:"failed"`
	OldestDueSeconds int64 `json:"oldestDueSeconds"`
	HistoricalActor  int64 `json:"historicalActor"`
	HistoricalAI     int64 `json:"historicalAI"`
	Budget           int64 `json:"budget"`
}
type QueueSnapshot struct {
	Scan     QueueGroup `json:"scan"`
	Pipeline QueueGroup `json:"pipeline"`
}
type QueueReader interface {
	ReadQueue(context.Context) (QueueSnapshot, error)
}
type sqlQueueReader struct {
	db         *sql.DB
	generation string
}

// NewQueueReader создаёт read-only представление текущего поколения.
func NewQueueReader(db *sql.DB, generation string) QueueReader {
	return &sqlQueueReader{db: db, generation: generation}
}

const queueSQL = `SELECT
 (SELECT generation FROM app_runtime_generation WHERE id=1),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='pending' AND run_after<=clock.now_ms THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='pending' AND run_after>clock.now_ms THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='running' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='failed' THEN 1 ELSE 0 END),0),
 COALESCE(MAX(CASE WHEN type='scan_source' AND status='pending' AND run_after<=clock.now_ms THEN clock.now_ms-run_after ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='pending' AND reason_code='historical_paid_unknown' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='pending' AND reason_code='historical_ai_unknown' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='scan_source' AND status='pending' AND reason_code='budget' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='pending' AND run_after<=clock.now_ms THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='pending' AND run_after>clock.now_ms THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='running' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='failed' THEN 1 ELSE 0 END),0),
 COALESCE(MAX(CASE WHEN type<>'scan_source' AND status='pending' AND run_after<=clock.now_ms THEN clock.now_ms-run_after ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='pending' AND reason_code='historical_paid_unknown' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='pending' AND reason_code='historical_ai_unknown' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type<>'scan_source' AND status='pending' AND reason_code='budget' THEN 1 ELSE 0 END),0)
 FROM scheduler_jobs CROSS JOIN (SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) AS now_ms) clock
 WHERE generation=? AND status IN ('pending','running','failed')
 AND type IN ('scan_source','classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance')`

// ReadQueue делает один bounded SELECT и не возвращает ID, payload или errors БД.
func (r *sqlQueueReader) ReadQueue(ctx context.Context) (QueueSnapshot, error) {
	if r == nil || r.db == nil || r.generation == "" || ctx == nil {
		return QueueSnapshot{}, errQueue
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var gen sql.NullString
	var out QueueSnapshot
	var scanMs, pipelineMs int64
	err := r.db.QueryRowContext(ctx, queueSQL, r.generation).Scan(&gen, &out.Scan.Due, &out.Scan.Delayed, &out.Scan.Running, &out.Scan.Failed, &scanMs, &out.Scan.HistoricalActor, &out.Scan.HistoricalAI, &out.Scan.Budget, &out.Pipeline.Due, &out.Pipeline.Delayed, &out.Pipeline.Running, &out.Pipeline.Failed, &pipelineMs, &out.Pipeline.HistoricalActor, &out.Pipeline.HistoricalAI, &out.Pipeline.Budget)
	if err != nil || !gen.Valid || gen.String != r.generation || scanMs < 0 || pipelineMs < 0 {
		return QueueSnapshot{}, errQueue
	}
	out.Scan.OldestDueSeconds = scanMs / 1000
	out.Pipeline.OldestDueSeconds = pipelineMs / 1000
	return out, nil
}
