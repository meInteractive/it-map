-- Sprint 1.1: READ-ONLY checks before enabling creation of CRM applications.
-- Run against a disposable MariaDB 10.11 staging clone of the PRIVATE dump.
-- Do not run writes on production and do not commit results containing names.

-- The application-create context is tied to exactly one runtime generation.
SELECT 'missing_generation' AS issue, CASE WHEN COUNT(*)=1
 AND MIN(id)=1 AND CHAR_LENGTH(MIN(generation))=36 THEN 0 ELSE 1 END AS groups_found
FROM app_runtime_generation;

-- Receipt PK and the generation/created-application columns must survive D1 conversion.
SELECT COLUMN_NAME,DATA_TYPE,IS_NULLABLE
FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()
 AND TABLE_NAME='application_creation_receipts'
 AND COLUMN_NAME IN ('user_name','idempotency_key','generation','request_hash','created_application_id')
ORDER BY ORDINAL_POSITION;

SELECT INDEX_NAME,NON_UNIQUE,COLUMN_NAME,SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='application_creation_receipts'
ORDER BY INDEX_NAME,SEQ_IN_INDEX;

-- All results below are aggregate counts, with no names or personal text.
SELECT 'receipt_missing_application' AS issue,COUNT(*) AS groups_found
FROM application_creation_receipts r LEFT JOIN company_applications a
 ON a.id=r.created_application_id AND a.user_name=r.user_name
WHERE r.created_application_id IS NOT NULL AND a.id IS NULL
UNION ALL
SELECT 'incomplete_receipt',COUNT(*) FROM application_creation_receipts
 WHERE created_application_id IS NULL
UNION ALL
SELECT 'duplicate_user_vacancy',COUNT(*) FROM (
 SELECT user_name,vacancy_id FROM company_applications WHERE vacancy_id IS NOT NULL
 GROUP BY user_name,vacancy_id HAVING COUNT(*)>1
) duplicates
UNION ALL
SELECT 'company_application_count_above_limit',COUNT(*) FROM (
 SELECT user_name,company_name FROM company_applications
 GROUP BY user_name,company_name HAVING COUNT(*)>10
) above_limit;

-- The initial Go writer coordinates via the company_crm PK and by locking
-- the singleton generation row. All other writers (including RabbitMQ/parser
-- and future backup/restore) must use the same discipline or compatible
-- database-level constraints before concurrent writes are enabled.
