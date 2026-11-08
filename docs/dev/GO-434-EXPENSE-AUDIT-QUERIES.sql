-- GO-434 | SELECT-only ledger reconnaissance for a verified read-only MariaDB 10.11 connection.
-- Do NOT run on production without explicit approval. No writes, DDL, API calls
-- or raw account IDs / vacancy URLs / user content are selected.
-- Results are private diagnostics: DO NOT commit their output to Git.
-- All-time grouped views below are intentionally unfiltered by runtime generation.
-- On a large DB, run on an authorized read replica or a scoped snapshot.

-- A. Apify local operation accounting: charged != reserve, NULL != zero.
SELECT profile,period_start,period_end,state,
       COUNT(*) operation_rows,
       SUM(CASE WHEN charged_usd IS NOT NULL THEN 1 ELSE 0 END) charged_rows,
       SUM(CASE WHEN charged_usd IS NOT NULL THEN charged_usd ELSE 0 END) recorded_charged_usd,
       SUM(CASE WHEN charged_usd IS NULL THEN reserved_usd ELSE 0 END) unresolved_reserved_usd,
       SUM(CASE WHEN charged_usd IS NULL THEN 1 ELSE 0 END) unresolved_rows,
       SUM(CASE WHEN run_id='' THEN 1 ELSE 0 END) missing_actor_run_id
FROM apify_runs
GROUP BY profile,period_start,period_end,state
ORDER BY period_start,profile,state;

-- B. Provider Actor identity collisions. One (account_id, run_id) should
-- represent one provider execution, not several independent invoices.
-- Identity is a truncated hash in output; confirm suspected collisions
-- against the private provider ledger before classifying paid repeats.
SELECT LEFT(SHA2(CONCAT(account_id,':',run_id),256),20) operation_fingerprint,
       COUNT(*) local_receipts,
       COUNT(DISTINCT generation) generations,
       COUNT(DISTINCT dataset_id) dataset_id_variants,
       SUM(CASE WHEN charged_usd IS NOT NULL THEN 1 ELSE 0 END) priced_receipts,
       MIN(charged_usd) min_charged_usd,
       MAX(charged_usd) max_charged_usd
FROM apify_runs
WHERE run_id<>''
GROUP BY account_id,run_id
HAVING COUNT(*)>1
ORDER BY local_receipts DESC,operation_fingerprint;

-- C. Lost Actor start ACK: can be reserved without being actually charged.
-- Do not rerun Actor to resolve this query.
SELECT profile,state,COUNT(*) unresolved_starts,
       SUM(reserved_usd) local_reserve_usd
FROM apify_runs
WHERE run_id=''
GROUP BY profile,state ORDER BY profile,state;

-- D. Scheduler attempt costs. Each ID counted ONCE; never add stage tables
-- to these totals. A cost=NULL, reserve=0 row is unpriced, not proved free.
SELECT COALESCE(provider,'unknown') provider,type,status,
       COUNT(*) attempts,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN 1 ELSE 0 END) priced_attempts,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0 END) local_confirmed_usd,
       SUM(CASE WHEN cost_usd IS NULL AND reserved_usd>0 THEN reserved_usd ELSE 0 END) unresolved_reserved_usd,
       SUM(CASE WHEN cost_usd IS NULL AND reserved_usd>0 THEN 1 ELSE 0 END) unresolved_attempts,
       SUM(CASE WHEN cost_usd IS NULL AND reserved_usd=0 THEN 1 ELSE 0 END) unpriced_attempts,
       SUM(CASE WHEN credits IS NOT NULL THEN credits ELSE 0 END) reported_credits
FROM scheduler_attempts
GROUP BY COALESCE(provider,'unknown'),type,status
ORDER BY provider,type,status;

-- E. Multiple scheduler attempts for the same JOB are possible paid repeats,
-- not evidence of duplicate provider billing. No raw job IDs in output.
SELECT LEFT(SHA2(job_id,256),20) job_fingerprint,COALESCE(provider,'unknown') provider,
       type,COUNT(*) attempts,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0 END) recorded_usd,
       SUM(CASE WHEN cost_usd IS NULL THEN reserved_usd ELSE 0 END) unresolved_reserved_usd
FROM scheduler_attempts
GROUP BY job_id,COALESCE(provider,'unknown'),type
HAVING COUNT(*)>1
ORDER BY attempts DESC,job_fingerprint;

-- F. Cardinality and completeness of secondary stage evidence ONLY. These
-- tables have no direct external provider request-ID linking all records
-- to scheduler_attempts. Stage sums are NOT additive to query D.
SELECT stage,COUNT(*) evidence_rows,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN 1 ELSE 0 END) rows_with_cost,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0 END) stage_cost_observed_usd
FROM (
    SELECT 'classification' stage,cost_usd FROM vacancy_classification_attempts
    UNION ALL
    SELECT 'extraction' stage,cost_usd FROM vacancy_extractions
    UNION ALL
    SELECT 'assign_roles' stage,cost_usd FROM vacancy_role_results
) stage_evidence
GROUP BY stage ORDER BY stage;

-- G. Firecrawl snapshot credit observations (NOT an invoice/charge ledger).
-- Cached/reused snapshots and unknown monetary rates cannot be treated as
-- zero USD; do not multiply credits by today's plan price.
SELECT firecrawl_mode,content_status,COUNT(*) snapshots,
       SUM(CASE WHEN firecrawl_credits IS NULL THEN 1 ELSE 0 END) unknown_credit_rows,
       SUM(CASE WHEN firecrawl_credits IS NOT NULL THEN firecrawl_credits ELSE 0 END) observed_snapshot_credits,
       COUNT(DISTINCT candidate_id) candidate_count
FROM vacancy_raw_snapshots
GROUP BY firecrawl_mode,content_status ORDER BY firecrawl_mode,content_status;

-- H. Raw successful import receipts versus unique NEW main vacancies.
-- Only create, not update/unchanged, counts toward the proposed denominator.
-- Repeated import receipts and multiple source candidates cannot multiply it.
SELECT action,COUNT(*) successful_receipts,
       COUNT(DISTINCT candidate_id) candidates,
       COUNT(DISTINCT CASE WHEN v.id IS NOT NULL THEN o.vacancy_id END) surviving_main_vacancies
FROM vacancy_import_operations o
LEFT JOIN company_vacancies v ON v.id=o.vacancy_id
WHERE o.status='success'
GROUP BY action ORDER BY action;

-- I. Publication lineage cardinality. A single main vacancy may have
-- multiple source candidates. Counts are not published-by-day or spend.
SELECT COUNT(*) source_links,
       COUNT(DISTINCT candidate_id) linked_candidates,
       COUNT(DISTINCT vacancy_id) distinct_main_vacancies,
       COUNT(DISTINCT extraction_id) distinct_extractions
FROM vacancy_publication_sources;

-- J. Scheduler attempt -> job -> candidate -> published link when available.
-- This does NOT make a scan/Actor cost directly attributable to every child.
-- Avoid joining candidate to MANY stage rows; those multiply cost sums.
SELECT COALESCE(a.provider,'unknown') provider,a.type,
       COUNT(DISTINCT a.id) attempts,
       SUM(CASE WHEN a.cost_usd IS NOT NULL THEN a.cost_usd ELSE 0 END) attempt_cost_usd,
       SUM(CASE WHEN a.cost_usd IS NULL THEN a.reserved_usd ELSE 0 END) unresolved_reserved_usd,
       SUM(CASE WHEN j.id IS NULL THEN 1 ELSE 0 END) missing_job,
       SUM(CASE WHEN j.candidate_id IS NULL THEN 1 ELSE 0 END) without_candidate,
       SUM(CASE WHEN p.vacancy_id IS NOT NULL THEN 1 ELSE 0 END) linked_to_main_vacancy
FROM scheduler_attempts a
LEFT JOIN scheduler_jobs j ON j.id=a.job_id
LEFT JOIN vacancy_publication_sources p ON p.candidate_id=j.candidate_id
GROUP BY COALESCE(a.provider,'unknown'),a.type
ORDER BY provider,type;

-- K. Repeated classifications of an unchanged title/model, aggregate only.
-- Different prompt versions and role groups are intentionally not proof
-- of a duplicate paid POST. Link to provider request ledger remains absent.
SELECT COUNT(*) repeated_groups,
       COALESCE(SUM(attempt_count),0) total_attempt_rows_in_groups,
       COALESCE(SUM(known_cost),0) stage_cost_observed_usd
FROM (
    SELECT candidate_id,title_hash,title_revision,model_key,
           COUNT(*) attempt_count,
           SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0 END) known_cost
    FROM vacancy_classification_attempts
    GROUP BY candidate_id,title_hash,title_revision,model_key
    HAVING COUNT(*)>1
) repeated_classification;
