-- Sprint 1.1.09: read-only MariaDB preflight, safe for staging.
-- The initial D1-to-MariaDB conversion did not preserve UNIQUE constraints
-- for identity name columns, which are LONGTEXT. Do NOT run blind inserts
-- into identity tables until a validated uniqueness migration is complete.
-- This SQL only returns counts and lengths: it does not expose private names.
SELECT 'company_identity' AS table_name, COUNT(*) AS rows_total,
       COUNT(DISTINCT company_name) AS unique_names,
       MAX(CHAR_LENGTH(company_name)) AS max_name_length
  FROM company_identity
UNION ALL
SELECT 'agency_identity', COUNT(*), COUNT(DISTINCT agency_name),
       MAX(CHAR_LENGTH(agency_name))
  FROM agency_identity;

SELECT 'company_user_state' AS table_name, COUNT(*) AS rows_total,
       COUNT(DISTINCT user_name, company_name) AS unique_keys,
       MAX(CHAR_LENGTH(user_name)) AS max_user_length
  FROM company_user_state;
