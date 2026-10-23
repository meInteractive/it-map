-- READ ONLY. Run on a SEPARATE MariaDB staging database restored from the
-- original private D1 dump. Do not publish row values, credentials or dumps.
-- The initial D1 -> MariaDB conversion has no unique key on
-- company_vacancy_user_state(user_name, vacancy_id).
-- This report only shows aggregate counts, not names or personal attributes.
SELECT COUNT(*) AS state_rows,
       COUNT(DISTINCT vacancy_id) AS referenced_vacancies
  FROM company_vacancy_user_state;

SELECT COUNT(*) AS duplicate_user_vacancy_pairs,
       COALESCE(SUM(row_count - 1),0) AS extra_state_rows
  FROM (
    SELECT user_name, vacancy_id, COUNT(*) AS row_count
      FROM company_vacancy_user_state
     GROUP BY user_name, vacancy_id
    HAVING COUNT(*) > 1
  ) AS duplicated;

SELECT COUNT(*) AS orphan_state_rows
  FROM company_vacancy_user_state AS s
  LEFT JOIN company_vacancies AS v ON v.id=s.vacancy_id
 WHERE v.id IS NULL;

SELECT COUNT(*) AS duplicate_personal_letter_pairs
  FROM (
    SELECT user_name, vacancy_id
      FROM vacancy_cover_letters
     GROUP BY user_name, vacancy_id
    HAVING COUNT(*) > 1
  ) AS duplicated;

SELECT COUNT(*) AS orphan_vacancy_telegram_sources
  FROM vacancy_telegram_sources AS s
  LEFT JOIN company_vacancies AS v ON v.id=s.vacancy_id
 WHERE v.id IS NULL;
