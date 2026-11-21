package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"itmap/internal/backup"
)

const hhVacancyJobMaxAge = 14 * 24 * time.Hour

func addHHJobPublication(payload json.RawMessage, publishedAt int64) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, err
	}
	stamp, err := json.Marshal(publishedAt)
	if err != nil {
		return nil, err
	}
	fields["hhPublishedAtMs"] = stamp
	value, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return backup.StringifyParsedJSON(value)
}

// Убираем только задания без предыдущих попыток. Отложенная попытка может
// иметь attempts=0, но всё ещё требовать сверки с платным провайдером.
func expireQueuedHHJobs(ctx context.Context, tx *sql.Tx, generation string, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE scheduler_jobs j
		SET status='cancelled', reason_code='hh_publication_expired', completed_at=?, updated_at=?
		WHERE j.generation=? AND j.source_id='hh' AND j.candidate_id IS NOT NULL
		AND j.type IN ('classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance')
		AND j.status='pending' AND j.attempts=0 AND j.locked_by='' AND j.locked_until=0
		AND JSON_TYPE(JSON_EXTRACT(j.payload,'$.hhPublishedAtMs'))='INTEGER'
		AND CAST(JSON_UNQUOTE(JSON_EXTRACT(j.payload,'$.hhPublishedAtMs')) AS SIGNED) BETWEEN 1 AND ?
		AND NOT EXISTS (SELECT 1 FROM scheduler_attempts a WHERE a.job_id=j.id)`,
		now, now, generation, now-hhVacancyJobMaxAge.Milliseconds()-1)
	if err != nil {
		return storageError(ctx)
	}
	return nil
}
