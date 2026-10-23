-- Read-only CRM preflight for staging MariaDB restored from the PRIVATE dump.
-- Run using a SELECT-only user; do not copy or publish returned rows.
-- These checks must be reviewed BEFORE allowing live Go CRM writes.

-- This direct D1 -> MariaDB conversion has no (user_name,company_name)
-- uniqueness for company_user_state. Duplicate groups must be reconciled.
SELECT COUNT(*) AS duplicated_user_company_groups FROM (
  SELECT user_name,company_name FROM company_user_state
  GROUP BY user_name,company_name HAVING COUNT(*)>1
) AS duplicates;

-- Duplicate user-vacancy applications should never be written through an
-- unverified Go API; each conflicting group requires case-by-case resolution.
SELECT COUNT(*) AS duplicate_application_links FROM (
  SELECT user_name,vacancy_id FROM company_applications
  WHERE vacancy_id IS NOT NULL GROUP BY user_name,vacancy_id HAVING COUNT(*)>1
) AS duplicates;

-- Dangling history records must be investigated before porting writes.
SELECT COUNT(*) AS orphan_application_history FROM company_application_status_history h
LEFT JOIN company_applications a ON a.id=h.application_id WHERE a.id IS NULL;

-- Count all user-facing data prior to staging regression testing. These are
-- aggregate values only; no personal details are selected by this preflight.
SELECT 'tasks' AS entity, COUNT(*) AS total FROM company_tasks
UNION ALL SELECT 'applications',COUNT(*) FROM company_applications
UNION ALL SELECT 'application_history',COUNT(*) FROM company_application_status_history
UNION ALL SELECT 'user_company_state',COUNT(*) FROM company_user_state;
