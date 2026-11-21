package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"itmap/internal/backup"
)

const hhVacancyJobMaxAge = 14 * 24 * time.Hour

var ErrHHPublicationExpired = errors.New("истёк срок обработки вакансии HH")

func hhJobPublication(payload json.RawMessage) int64 {
	var metadata struct {
		PublishedAt int64 `json:"hhPublishedAtMs"`
	}
	if json.Unmarshal(payload, &metadata) != nil {
		return 0
	}
	return metadata.PublishedAt
}

func expiredHHJob(job Job, now time.Time) bool {
	publishedAt := hhJobPublication(job.Payload)
	return job.SourceID != nil && *job.SourceID == "hh" && publishedAt > 0 &&
		publishedAt < now.Add(-hhVacancyJobMaxAge).UnixMilli()
}

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

// Для Apify fulltext история scheduler_attempts сама по себе не подтверждает
// сверку отдельного ledger. Для остальных стадий требуется итог каждой попытки.
func expireQueuedHHJobs(ctx context.Context, tx *sql.Tx, generation string, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE scheduler_jobs j
		SET status='cancelled', reason_code='hh_publication_expired', completed_at=?, updated_at=?
		WHERE j.generation=? AND j.source_id='hh' AND j.candidate_id IS NOT NULL
		AND j.type IN ('classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance')
		AND j.status='pending' AND j.locked_by='' AND j.locked_until=0
		AND JSON_TYPE(JSON_EXTRACT(j.payload,'$.hhPublishedAtMs'))='INTEGER'
		AND CAST(JSON_UNQUOTE(JSON_EXTRACT(j.payload,'$.hhPublishedAtMs')) AS SIGNED) BETWEEN 1 AND ?
		AND (
		  (j.attempts=0 AND NOT EXISTS (SELECT 1 FROM scheduler_attempts a WHERE a.job_id=j.id))
		  OR (j.type<>'fetch_full_text'
		    AND EXISTS (SELECT 1 FROM scheduler_attempts a WHERE a.job_id=j.id)
		    AND NOT EXISTS (SELECT 1 FROM scheduler_attempts a WHERE a.job_id=j.id
		      AND (a.type<>j.type OR a.status NOT IN ('completed','failed')
		        OR a.finished_at IS NULL OR a.finished_at<a.started_at OR a.finished_at>?
		        OR a.cost_usd IS NULL
		        OR NOT EXISTS (SELECT 1 FROM scheduler_runs r WHERE r.id=a.run_id AND r.generation=j.generation))))
		)`,
		now, now, generation, now-hhVacancyJobMaxAge.Milliseconds()-1, now)
	if err != nil {
		return storageError(ctx)
	}
	return nil
}
