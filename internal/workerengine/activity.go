package workerengine

import (
	"context"
	"database/sql"
	"time"
)

// ActivitySnapshot counts stored events in the current generation during the
// preceding 24 hours. It cannot attribute work to a particular consumer or
// demonstrate that a candidate was eventually published.
type ActivitySnapshot struct {
	ActorStarted               int64 `json:"actorStarted"`
	ActorSucceededCostRecorded int64 `json:"actorSucceededCostRecorded"`
	ActorActiveOrCostMissing   int64 `json:"actorActiveOrCostMissing"`
	ScanCompleted              int64 `json:"scanCompleted"`
	PipelineCreated            int64 `json:"pipelineCreated"`
	PipelineCompleted          int64 `json:"pipelineCompleted"`
	PipelineFailed             int64 `json:"pipelineFailed"`
}
type ActivityReader interface {
	ReadActivity(context.Context) (ActivitySnapshot, error)
}
type sqlActivityReader struct {
	db         *sql.DB
	generation string
}

func NewActivityReader(db *sql.DB, generation string) ActivityReader {
	return &sqlActivityReader{db: db, generation: generation}
}

const activitySQL = `SELECT g.generation,
 (SELECT COUNT(*) FROM apify_runs a WHERE a.generation=g.generation AND a.created_at>=clock.now_ms-86400000 AND a.created_at<=clock.now_ms),
 (SELECT COUNT(*) FROM apify_runs a WHERE a.generation=g.generation AND a.created_at>=clock.now_ms-86400000 AND a.created_at<=clock.now_ms AND a.state='succeeded' AND a.charged_usd IS NOT NULL),
 (SELECT COUNT(*) FROM apify_runs a WHERE a.generation=g.generation AND a.created_at>=clock.now_ms-86400000 AND a.created_at<=clock.now_ms AND (a.charged_usd IS NULL OR a.state IN ('starting','running','uncertain'))),
 (SELECT COUNT(*) FROM scheduler_jobs j WHERE j.generation=g.generation AND j.type='scan_source' AND j.status='completed' AND j.completed_at>=clock.now_ms-86400000 AND j.completed_at<=clock.now_ms),
 (SELECT COUNT(*) FROM scheduler_jobs j WHERE j.generation=g.generation AND j.type IN ('classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance') AND j.created_at>=clock.now_ms-86400000 AND j.created_at<=clock.now_ms),
 (SELECT COUNT(*) FROM scheduler_jobs j WHERE j.generation=g.generation AND j.type IN ('classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance') AND j.status='completed' AND j.completed_at>=clock.now_ms-86400000 AND j.completed_at<=clock.now_ms),
 (SELECT COUNT(*) FROM scheduler_jobs j WHERE j.generation=g.generation AND j.type IN ('classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance') AND j.status='failed' AND j.completed_at>=clock.now_ms-86400000 AND j.completed_at<=clock.now_ms)
 FROM app_runtime_generation g CROSS JOIN (SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) AS now_ms) clock WHERE g.id=1`

func (r *sqlActivityReader) ReadActivity(ctx context.Context) (ActivitySnapshot, error) {
	var out ActivitySnapshot
	if r == nil || r.db == nil || r.generation == "" || ctx == nil {
		return out, errQueue
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var generation string
	err := r.db.QueryRowContext(ctx, activitySQL).Scan(&generation, &out.ActorStarted, &out.ActorSucceededCostRecorded, &out.ActorActiveOrCostMissing, &out.ScanCompleted, &out.PipelineCreated, &out.PipelineCompleted, &out.PipelineFailed)
	if err != nil || generation != r.generation || ctx.Err() != nil {
		return ActivitySnapshot{}, errQueue
	}
	return out, nil
}
