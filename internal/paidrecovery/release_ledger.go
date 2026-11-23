package paidrecovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

const releaseLedgerLimit = 1024

// ReleaseLedger содержит только количество и digest старых AI-попыток.
type ReleaseLedger struct {
	Count  int    `json:"count"`
	Digest string `json:"digest"`
}

// ReadReleaseLedger под блокировкой поколения фиксирует состав и состояние
// закрытых локально попыток. Стоимость этих попыток остаётся неизвестной.
func ReadReleaseLedger(ctx context.Context, db *sql.DB, generation string) (ReleaseLedger, error) {
	return readReleaseLedger(ctx, db, generation, false)
}

// ReadReleaseLedgerWithFirecrawl also checks the paid scrape concurrency window.
func ReadReleaseLedgerWithFirecrawl(ctx context.Context, db *sql.DB, generation string) (ReleaseLedger, error) {
	return readReleaseLedger(ctx, db, generation, true)
}

func readReleaseLedger(ctx context.Context, db *sql.DB, generation string, checkFirecrawl bool) (ReleaseLedger, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ReleaseLedger{}, err
	}
	defer tx.Rollback()
	var actual string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual); err != nil || actual != generation {
		return ReleaseLedger{}, ErrUnavailable
	}
	if checkFirecrawl {
		var firecrawlActive bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM firecrawl_usage_events
		WHERE active_until > CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED))`).Scan(&firecrawlActive)
		if err != nil || firecrawlActive {
			return ReleaseLedger{}, ErrUnavailable
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT SHA2(JSON_ARRAY(a.id,a.job_id,a.run_id,a.type,a.provider,a.model,
		a.status,a.reason_code,a.started_at,a.finished_at,a.reserved_usd,a.credits,
		SHA2(a.result_json,256),j.status,j.locked_by,j.locked_until,r.status,r.lease_until),256),
		COALESCE(a.status IN ('completed','failed','interrupted','pending')
		  AND a.finished_at>=a.started_at AND a.started_at>0
		  AND a.finished_at<=clock.now_ms-1800000
		  AND j.generation=r.generation AND j.status<>'running'
		  AND j.locked_by='' AND j.locked_until=0
		  AND r.status<>'running',0)
		FROM scheduler_attempts a JOIN scheduler_runs r ON r.id=a.run_id
		LEFT JOIN scheduler_jobs j ON j.id=a.job_id
		CROSS JOIN (SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) AS now_ms) clock
		WHERE BINARY r.generation=BINARY ? AND a.provider IN ('openai','openrouter') AND a.cost_usd IS NULL
		  AND (a.reserved_usd>0 OR (a.reserved_usd=0 AND a.type IN ('classify_title','extract_vacancy','assign_roles','score_relevance')))
		ORDER BY a.id LIMIT 1025`, generation)
	if err != nil {
		return ReleaseLedger{}, err
	}
	defer rows.Close()
	hash := sha256.New()
	_, _ = hash.Write([]byte(generation))
	var out ReleaseLedger
	for rows.Next() {
		var digest string
		var stable bool
		if err = rows.Scan(&digest, &stable); err != nil || !stable || len(digest) != 64 || out.Count == releaseLedgerLimit {
			return ReleaseLedger{}, ErrUnavailable
		}
		out.Count++
		_, _ = hash.Write([]byte(digest))
	}
	if err = rows.Err(); err != nil {
		return ReleaseLedger{}, err
	}
	out.Digest = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}
