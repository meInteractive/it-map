-- Run only on a private MariaDB 10.11 staging clone before permitting
-- DELETE /api/vacancies/{id}. Counts only: never print real row values.
-- Abort deployment if the expected constraints differ or existing relations
-- are orphaned. Do NOT use this script to repair production automatically.
SELECT 'orphaned_applications' AS check_name, COUNT(*) AS violations
FROM company_applications a LEFT JOIN company_vacancies v ON v.id=a.vacancy_id
WHERE a.vacancy_id IS NOT NULL AND v.id IS NULL;
SELECT 'orphaned_tasks' AS check_name, COUNT(*) AS violations
FROM company_tasks t LEFT JOIN company_vacancies v ON v.id=t.vacancy_id
WHERE t.vacancy_id IS NOT NULL AND v.id IS NULL;
SELECT 'unexpected_fk_actions' AS check_name,COUNT(*) AS violations
FROM information_schema.REFERENTIAL_CONSTRAINTS
WHERE CONSTRAINT_SCHEMA=DATABASE() AND REFERENCED_TABLE_NAME='company_vacancies'
AND TABLE_NAME IN ('company_applications','company_tasks')
AND DELETE_RULE <> 'SET NULL';
SELECT 'missing_set_null_fks' AS check_name,
  2-COUNT(DISTINCT TABLE_NAME) AS violations
FROM information_schema.REFERENTIAL_CONSTRAINTS
WHERE CONSTRAINT_SCHEMA=DATABASE() AND REFERENCED_TABLE_NAME='company_vacancies'
AND TABLE_NAME IN ('company_applications','company_tasks')
AND DELETE_RULE='SET NULL';
