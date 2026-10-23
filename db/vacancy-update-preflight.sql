-- Read-only staging preflight for PUT /api/vacancies/{id} on the restored
-- PRIVATE MariaDB dump. No data, credentials or results should be committed.
-- An individual duplicate group is not repaired automatically by the writer.
SELECT 'duplicate_vacancy_url' AS issue, COUNT(*) AS groups_found FROM (
 SELECT url FROM company_vacancies GROUP BY url HAVING COUNT(*) > 1
) d
UNION ALL
SELECT 'duplicate_personal_letter', COUNT(*) FROM (
 SELECT user_name,vacancy_id FROM vacancy_cover_letters
 GROUP BY user_name,vacancy_id HAVING COUNT(*) > 1
) d
UNION ALL
SELECT 'duplicate_telegram_channel_link', COUNT(*) FROM (
 SELECT vacancy_id,channel_id FROM vacancy_telegram_sources
 GROUP BY vacancy_id,channel_id HAVING COUNT(*) > 1
) d
UNION ALL
SELECT 'duplicate_telegram_message_url', COUNT(*) FROM (
 SELECT message_url FROM vacancy_telegram_sources GROUP BY message_url HAVING COUNT(*) > 1
) d
UNION ALL
SELECT 'duplicate_vacancy_user_state', COUNT(*) FROM (
 SELECT user_name,vacancy_id FROM company_vacancy_user_state
 GROUP BY user_name,vacancy_id HAVING COUNT(*) > 1
) d;

-- These FK actions must be reviewed before enabling edits of ownership,
-- roles, Telegram associations or personal letters.
SELECT k.TABLE_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_NAME,
       k.REFERENCED_COLUMN_NAME,r.UPDATE_RULE,r.DELETE_RULE
FROM information_schema.KEY_COLUMN_USAGE k
JOIN information_schema.REFERENTIAL_CONSTRAINTS r
  ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA
 AND r.TABLE_NAME=k.TABLE_NAME
 AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME
WHERE k.CONSTRAINT_SCHEMA=DATABASE()
 AND k.TABLE_NAME IN ('company_vacancies','company_applications','company_tasks',
  'vacancy_cover_letters','vacancy_role_assignments','vacancy_telegram_sources',
  'company_vacancy_user_state')
 AND k.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY k.TABLE_NAME,k.COLUMN_NAME;
