-- Read-only preflight for a PRIVATE STAGING copy of the 2026-10-23 MariaDB dump.
-- Never include actual query rows in public CI logs: URL and channel names can
-- contain private data. A nonzero count requires manual reconciliation.
-- Counts only, never prints data or modifies tables.
SELECT 'duplicate_vacancy_urls' AS check_name, COUNT(*) AS violations
FROM (SELECT url FROM company_vacancies GROUP BY url HAVING COUNT(*) > 1) v;

SELECT 'duplicate_telegram_message_urls' AS check_name, COUNT(*) AS violations
FROM (SELECT message_url FROM vacancy_telegram_sources GROUP BY message_url HAVING COUNT(*) > 1) s;

SELECT 'orphaned_role_assignments' AS check_name, COUNT(*) AS violations
FROM vacancy_role_assignments a
LEFT JOIN company_vacancies v ON v.id=a.vacancy_id
LEFT JOIN vacancy_roles r ON r.id=a.role_id
WHERE v.id IS NULL OR r.id IS NULL;

SELECT 'non_leaf_assignments' AS check_name, COUNT(*) AS violations
FROM vacancy_role_assignments a
WHERE EXISTS(SELECT 1 FROM vacancy_roles child WHERE child.parent_id=a.role_id);

SELECT 'orphaned_telegram_sources' AS check_name, COUNT(*) AS violations
FROM vacancy_telegram_sources s
LEFT JOIN company_vacancies v ON v.id=s.vacancy_id
LEFT JOIN telegram_channels c ON c.id=s.channel_id
WHERE v.id IS NULL OR c.id IS NULL;

SELECT 'duplicate_user_vacancy_states' AS check_name, COUNT(*) AS violations
FROM (SELECT user_name,vacancy_id FROM company_vacancy_user_state
      GROUP BY user_name,vacancy_id HAVING COUNT(*) > 1) s;
