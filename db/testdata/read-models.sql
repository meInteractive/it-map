-- SYNTHETIC CI DATA ONLY. Load AFTER storage tests and smoke-catalog.sql,
-- ONLY into the disposable itmap_ci service. Eugene/Demo are test identities.
-- Demo intentionally has no private records: reads must not fall back to Eugene.
INSERT INTO `company_vacancies` (`id`, `company_name`, `url`, `title`) VALUES (1, 'CI Synthetic Employer', 'https://example.invalid/ci-vacancy', 'CI Synthetic Vacancy');
INSERT INTO `company_applications` (`id`, `company_name`, `vacancy_id`, `user_name`, `application_sources`) VALUES (1, 'CI Synthetic Employer', 1, 'Eugene', '["site"]');
INSERT INTO `company_application_status_history` (`id`, `application_id`, `from_status`, `to_status`) VALUES (1, 1, '', 'Откликнулся');
INSERT INTO `company_tasks` (`id`, `company_name`, `vacancy_id`, `user_name`, `type`, `note`, `due_at`) VALUES (1, 'CI Synthetic Employer', 1, 'Eugene', 'CI synthetic task', 'CI synthetic note', '2026-01-01T12:00:00.000Z');
