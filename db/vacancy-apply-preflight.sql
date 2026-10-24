-- Sprint 1.1 V12: read-only diagnostics for vacancy-card one-click applications.
-- Execute ONLY against an isolated MariaDB 10.11 staging copy of the private dump.
-- Never commit the result rows (which can expose internal IDs/user state).
-- The shortcut uses app_runtime_generation + per-company row + vacancy row locks.

SELECT COUNT(*) AS runtime_generation_rows FROM app_runtime_generation WHERE id = 1;
SELECT COUNT(*) AS receipt_rows_with_missing_application
FROM application_creation_receipts r
LEFT JOIN company_applications a ON a.id = r.created_application_id AND a.user_name = r.user_name
WHERE r.created_application_id IS NOT NULL AND a.id IS NULL;

-- Multiple applications for the same user/vacancy are ambiguous: Go fails closed.
SELECT COUNT(*) AS ambiguous_user_vacancy_groups FROM (
  SELECT user_name, vacancy_id
  FROM company_applications
  WHERE vacancy_id IS NOT NULL
  GROUP BY user_name, vacancy_id
  HAVING COUNT(*) > 1
) duplicated;

SELECT COUNT(*) AS orphaned_vacancy_links
FROM company_applications a
LEFT JOIN company_vacancies v ON v.id = a.vacancy_id
WHERE a.vacancy_id IS NOT NULL AND v.id IS NULL;

-- Missing owner names prevent trusting the shortcut's companyName claim.
SELECT COUNT(*) AS vacancies_with_missing_owner
FROM company_vacancies
WHERE company_name IS NULL OR TRIM(company_name) = '';

-- Confirm the intended receipt uniqueness and transaction-capable engine.
SELECT table_name, engine
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name IN ('application_creation_receipts','company_applications',
                     'company_crm','company_vacancies','app_runtime_generation')
ORDER BY table_name;
