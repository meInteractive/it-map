-- CI/STAGING ONLY: SCHEMA-ONLY test fixture; zero rows, no credentials or user data.
-- Generated from the 2026-10-23 MariaDB schema; do not use for production, do not import into existing DB.
-- Import only into a newly created, disposable itmap_ci database.
-- MariaDB 10.11+. Import into a NEW EMPTY database. No DROP statements.
SET @d1_old_sql_mode=@@SESSION.sql_mode; SET @d1_old_time_zone=@@SESSION.time_zone;
SET SESSION sql_mode='STRICT_ALL_TABLES,NO_AUTO_VALUE_ON_ZERO'; SET SESSION time_zone='+00:00'; SET NAMES utf8mb4 COLLATE utf8mb4_nopad_bin;
CREATE TABLE `__appgarden_migrations` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `applied_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `agency_identity` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `agency_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `apify_account_usage` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `account_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_start` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_end` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `provider_used_usd` DOUBLE NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (provider_used_usd>=0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `apify_runs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `operation_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `profile` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `account_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `key_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_start` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_end` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `reserved_usd` DOUBLE NOT NULL,
  `charged_usd` DOUBLE NULL,
  `state` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `run_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `dataset_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (profile IN ('free','starter')),
  CHECK (reserved_usd>0),
  CHECK (charged_usd IS NULL OR charged_usd>=0),
  CHECK (state IN ('starting','running','succeeded','failed','uncertain'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `apify_settings` (
  `id` BIGINT NOT NULL,
  `enabled` BIGINT NOT NULL DEFAULT 0,
  `paid_allowed` BIGINT NOT NULL DEFAULT 0,
  `revision` BIGINT NOT NULL DEFAULT 0,
  `free_key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `free_key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `starter_key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `starter_key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `free_account_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `starter_account_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `free_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `starter_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CHECK (id=1),
  CHECK (enabled IN (0,1)),
  CHECK (paid_allowed IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `app_catalog` (
  `id` BIGINT NOT NULL,
  `data_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `app_runtime_generation` (
  `id` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CHECK (`id` = 1),
  CHECK (CHAR_LENGTH(`generation`) = 36)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `application_creation_receipts` (
  `user_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `idempotency_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `request_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `created_application_id` BIGINT NULL,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_name`,`idempotency_key`),
  CHECK (CHAR_LENGTH(`user_name`) BETWEEN 1 AND 160),
  CHECK (CHAR_LENGTH(`idempotency_key`) = 36),
  CHECK (CHAR_LENGTH(`generation`) = 36),
  CHECK (CHAR_LENGTH(`request_hash`) = 64 AND `request_hash` NOT REGEXP '[^0-9a-f]'),
  CHECK (`created_application_id` IS NULL OR `created_application_id` > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_application_status_history` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `application_id` BIGINT NOT NULL,
  `from_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `to_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `changed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_applications` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `vacancy_id` BIGINT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'Откликнулся',
  `comment_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `recruiter_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `recruiter_phone` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `recruiter_email` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `recruiter_contact_type` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `application_sources` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '[`hh`]',
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'Eugene',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_crm` (
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `comment_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `favorite` BIGINT NOT NULL DEFAULT false,
  PRIMARY KEY (`company_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_identity` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_status_history` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `from_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `to_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `changed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'Eugene',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_tasks` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `type` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `note` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `due_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `completed` BIGINT NOT NULL DEFAULT false,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `completed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `vacancy_id` BIGINT NULL,
  `google_event_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `google_event_html_link` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `google_sync_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `google_sync_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'Eugene',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_user_state` (
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `comment_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `favorite` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `hidden` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_vacancies` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `salary_from` BIGINT NULL,
  `salary_to` BIGINT NULL,
  `title` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `published_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `description_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `application_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `level` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `loaded_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `role` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `relevance` BIGINT NULL,
  `entity_type` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'company',
  `real_company_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `cover_letter_subject` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `cover_letter_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `last_checked_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `archived` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  CHECK (`relevance` IS NULL OR (`relevance` >= 0 AND `relevance` <= 100))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_vacancy_state` (
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `initialized_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`company_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `company_vacancy_user_state` (
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `vacancy_id` BIGINT NOT NULL,
  `relevance` BIGINT NULL,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `favorite` BIGINT NOT NULL DEFAULT false,
  `hidden` BIGINT NOT NULL DEFAULT false,
  CHECK (`relevance` is null or (`relevance` between 0 and 100))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `cover_letter_generation_leases` (
  `user_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `vacancy_id` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `input_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`user_name`,`vacancy_id`),
  CHECK (CHAR_LENGTH(user_name) BETWEEN 1 AND 160),
  CHECK (vacancy_id > 0),
  CHECK (CHAR_LENGTH(generation) = 36),
  CHECK (CHAR_LENGTH(input_hash) = 64 AND input_hash NOT REGEXP '[^0-9a-f]'),
  CHECK (CHAR_LENGTH(owner_token) = 36),
  CHECK (expires_at > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `firecrawl_request_lease` (
  `id` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (id=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `firecrawl_settings` (
  `id` BIGINT NOT NULL,
  `key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `enabled` BIGINT NOT NULL DEFAULT 0,
  `max_pages` BIGINT NOT NULL DEFAULT 3,
  `concurrency` BIGINT NOT NULL DEFAULT 1,
  `timeout_ms` BIGINT NOT NULL DEFAULT 45000,
  `proxy` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'basic',
  `enhanced_allowed` BIGINT NOT NULL DEFAULT 0,
  `revision` BIGINT NOT NULL DEFAULT 0,
  `connection_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'missing',
  `last_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `checked_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `mode` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'free',
  `hobby_key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `hobby_key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `hobby_connection_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'missing',
  `hobby_last_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `hobby_checked_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `free_key_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `hobby_key_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `free_usage_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `hobby_usage_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `favorites_only` BIGINT NOT NULL DEFAULT 1,
  `scheduler_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '{`enabled`:false,`ownerName`:`Eugene`,`dayBudgetUsd`:0,`monthBudgetUsd`:0,`revision`:0,`cloudOwnerId`:"`,`priorities`:{`import_vacancy`:100,`assign_roles`:90,`extract_vacancy`:80,`fetch_full_text`:70,`classify_title`:60,`scan_source`:20,`diagnostic":0}}',
  PRIMARY KEY (`id`),
  CHECK (id=1),
  CHECK (enabled IN (0,1)),
  CHECK (max_pages BETWEEN 1 AND 10),
  CHECK (concurrency=1),
  CHECK (timeout_ms BETWEEN 5000 AND 60000),
  CHECK (proxy IN ('basic','enhanced')),
  CHECK (enhanced_allowed IN (0,1)),
  CHECK (mode IN ('free','hobby')),
  CHECK (favorites_only IN (0,1)),
  CHECK (json_valid(scheduler_json)),
  CHECK (proxy='basic' OR (mode='hobby' AND enhanced_allowed=1)),
  CHECK (enabled=0 OR (mode='free' AND CHAR_LENGTH(key_encrypted)>0) OR (mode='hobby' AND CHAR_LENGTH(hobby_key_encrypted)>0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `firecrawl_usage_events` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `key_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `profile` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_start` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_end` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `operation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `reserved_credits` BIGINT NOT NULL,
  `charged_credits` DOUBLE NULL,
  `outcome` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `started_at` BIGINT NOT NULL,
  `finished_at` BIGINT NULL,
  `active_until` BIGINT NOT NULL,
  `provider_status` BIGINT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (profile IN ('free','hobby')),
  CHECK (operation='scrape'),
  CHECK (reserved_credits>0),
  CHECK (charged_credits IS NULL OR charged_credits>=0),
  CHECK (outcome IN ('reserved','complete','error','uncertain'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `firecrawl_usage_periods` (
  `key_hash` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_start` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `period_end` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `provider_used_high` BIGINT NOT NULL DEFAULT 0,
  `remaining_credits` BIGINT NOT NULL,
  `plan_credits` BIGINT NOT NULL,
  `checked_at` BIGINT NOT NULL,
  PRIMARY KEY (`key_hash`,`period_start`,`period_end`),
  CHECK (provider_used_high>=0),
  CHECK (remaining_credits>=0),
  CHECK (plan_credits>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `google_calendar_connection` (
  `id` BIGINT NOT NULL,
  `account_email` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `refresh_token_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `calendar_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'primary',
  `connected_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `google_calendar_user_connection` (
  `user_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `account_email` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `refresh_token_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `calendar_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'primary',
  `connected_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `hiring_contacts` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `position` BIGINT NOT NULL,
  `full_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `linkedin_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `facebook_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `telegram` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `phone` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `email` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `specialization` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `setka_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CHECK (`position` between 1 and 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `openai_settings` (
  `id` BIGINT NOT NULL,
  `key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CHECK (`id` = 1),
  CHECK (`revision` >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `openrouter_ai_logs` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `task` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `requested_primary_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `actual_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `fallback_used` BIGINT NOT NULL DEFAULT 0,
  `success` BIGINT NOT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `openrouter_model_metrics` (
  `model_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `success_count` BIGINT NOT NULL DEFAULT 0,
  `total_duration_ms` BIGINT NOT NULL DEFAULT 0,
  `last_duration_ms` BIGINT NULL,
  `last_success_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `failure_count` BIGINT NOT NULL DEFAULT 0,
  `last_failure_duration_ms` BIGINT NULL,
  `last_failure_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  PRIMARY KEY (`model_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `openrouter_settings` (
  `id` BIGINT NOT NULL,
  `key_encrypted` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `key_suffix` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `connection_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'missing',
  `last_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `cache_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '[]',
  `primary_model_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `models_synced_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `last_used_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `last_used_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `revision` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `parser_candidate_intakes` (
  `candidate_token` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `parser` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `received_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`candidate_token`),
  CHECK (parser IN ('apify','firecrawl','http','manual','excel','unknown'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `parser_error_daily` (
  `owner_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `day` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `p1_peak` BIGINT NOT NULL,
  `p2_peak` BIGINT NOT NULL,
  `first_sample_at` BIGINT NOT NULL,
  `last_sample_at` BIGINT NOT NULL,
  PRIMARY KEY (`owner_name`,`day`),
  CHECK (CHAR_LENGTH(day)=10 AND day REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
  CHECK (p1_peak>=0),
  CHECK (p2_peak>=0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `prompt_model_settings` (
  `kind` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'openrouter',
  `openai_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `reasoning_effort` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'medium',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `openrouter_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'best_free',
  PRIMARY KEY (`kind`),
  CHECK (openrouter_model IN ('best_free','jev') AND (kind = 'relevance' OR openrouter_model = 'best_free')),
  CHECK (`kind` IN ('cover_letter','relevance')),
  CHECK (`provider` IN ('openrouter','openai')),
  CHECK (CHAR_LENGTH(`openai_model`) <= 160),
  CHECK (`reasoning_effort` IN ('auto','none','minimal','low','medium','high','xhigh','max')),
  CHECK (`revision` >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `scheduler_attempts` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `job_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `run_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `type` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `company_id` BIGINT NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `started_at` BIGINT NOT NULL,
  `finished_at` BIGINT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `reason_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `message` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `reserved_usd` DOUBLE NOT NULL DEFAULT 0,
  `cost_usd` DOUBLE NULL,
  `credits` DOUBLE NULL,
  `result_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '{}',
  PRIMARY KEY (`id`),
  CHECK (reserved_usd>=0),
  CHECK (cost_usd IS NULL OR cost_usd>=0),
  CHECK (json_valid(result_json))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `scheduler_jobs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `dedupe_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `type` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `company_id` BIGINT NULL,
  `candidate_id` BIGINT NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `payload` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '{}',
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'pending',
  `priority` BIGINT NOT NULL,
  `run_after` BIGINT NOT NULL,
  `attempts` BIGINT NOT NULL DEFAULT 0,
  `max_attempts` BIGINT NOT NULL DEFAULT 4,
  `locked_by` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `locked_until` BIGINT NOT NULL DEFAULT 0,
  `last_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `reason_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  `completed_at` BIGINT NULL,
  `agency_id` BIGINT NULL,
  `_d1_active_scan` TINYINT GENERATED ALWAYS AS (CASE WHEN type='scan_source' AND status IN ('pending','running') THEN 1 ELSE NULL END) VIRTUAL INVISIBLE,
  PRIMARY KEY (`id`),
  CHECK (type IN ('scan_source','classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance','diagnostic')),
  CHECK (json_valid(payload)),
  CHECK (status IN ('pending','running','completed','failed','cancelled')),
  CHECK (agency_id IS NULL OR company_id IS NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `scheduler_runs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `trigger` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `started_at` BIGINT NOT NULL,
  `finished_at` BIGINT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `ready` BIGINT NOT NULL DEFAULT 0,
  `processed` BIGINT NOT NULL DEFAULT 0,
  `succeeded` BIGINT NOT NULL DEFAULT 0,
  `errors` BIGINT NOT NULL DEFAULT 0,
  `remaining` BIGINT NOT NULL DEFAULT 0,
  `reason` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `cursor_id` BIGINT NOT NULL DEFAULT 0,
  `lease_until` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `system_prompts` (
  `kind` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `markdown` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`kind`),
  CHECK (`kind` IN ('cover_letter','relevance')),
  CHECK (CHAR_LENGTH(`markdown`) <= CASE WHEN `kind` = 'cover_letter' THEN 15000 ELSE 5000 END),
  CHECK (`revision` >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `telegram_channels` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `normalized_username` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `username` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `last_successful_check_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `comment` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CHECK (CHAR_LENGTH(`comment`) <= 512)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `user_profiles` (
  `user_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `resume_md` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_name`),
  CHECK (CHAR_LENGTH(`resume_md`) <= 20000),
  CHECK (`revision` >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_audit_runs` (
  `run_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `deleted_count` BIGINT NOT NULL,
  `publication_date_update_count` BIGINT NOT NULL,
  `completed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`run_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_candidate_classification_leases` (
  `candidate_id` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`candidate_id`),
  CHECK (expires_at>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_candidate_identities` (
  `identity_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  PRIMARY KEY (`identity_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_candidates` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `creation_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `company_id` BIGINT NULL,
  `company_name_raw` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `title_raw` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_revision` BIGINT NOT NULL DEFAULT 1,
  `vacancy_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `canonical_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `source_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `source_namespace` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `external_job_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `import_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `acquisition_method` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `first_seen_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `last_seen_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `main_vacancy_id` BIGINT NULL,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `agency_id` BIGINT NULL,
  PRIMARY KEY (`id`),
  CHECK (CHAR_LENGTH(trim(title_raw)) BETWEEN 1 AND 500),
  CHECK (CHAR_LENGTH(title_hash)=64),
  CHECK (title_revision>=1),
  CHECK (acquisition_method IN ('site','firecrawl','excel','manual'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_classification_attempts` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  `title_raw` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_revision` BIGINT NOT NULL,
  `model_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `model_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `requested_roles_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `errors_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '[]',
  `outcome` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `classified_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `duration_ms` BIGINT NULL,
  `cost_usd` DOUBLE NULL,
  PRIMARY KEY (`id`),
  CHECK (CHAR_LENGTH(title_hash)=64),
  CHECK (json_valid(requested_roles_json)),
  CHECK (json_valid(errors_json)),
  CHECK (outcome IN ('complete','partial','error')),
  CHECK (duration_ms IS NULL OR duration_ms>=0),
  CHECK (cost_usd IS NULL OR cost_usd>=0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_classification_jobs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `request_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `state` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'queued',
  `items_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `lease_until` BIGINT NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  `duration_ms` BIGINT NOT NULL DEFAULT 0,
  `reason` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CHECK (state IN ('queued','running','paused','completed')),
  CHECK (json_valid(items_json))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_classification_slots` (
  `slot` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`slot`),
  CHECK (slot BETWEEN 1 AND 2)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_classifications` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `candidate_id` BIGINT NOT NULL,
  `attempt_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `root_role_id` BIGINT NOT NULL,
  `root_role_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `decision` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `model_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `model_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_version` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_revision` BIGINT NOT NULL,
  `title_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `title_revision` BIGINT NOT NULL,
  `classified_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (decision IN ('match','reject')),
  CHECK (CHAR_LENGTH(prompt_version)=64),
  CHECK (CHAR_LENGTH(title_hash)=64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_content_fetches` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  `snapshot_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `source_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `final_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `firecrawl_mode` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `credits` DOUBLE NULL,
  `started_at` BIGINT NOT NULL,
  `duration_ms` BIGINT NOT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `message` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CHECK (status IN ('success','error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_cover_letters` (
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `vacancy_id` BIGINT NOT NULL,
  `subject` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `markdown` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `override_shared` BIGINT NOT NULL DEFAULT 0,
  `revision` BIGINT NOT NULL DEFAULT 0,
  CHECK (`override_shared` in (0, 1)),
  CHECK (`revision` >= 0),
  CHECK (CHAR_LENGTH(`subject`) between 1 and 512),
  CHECK (CHAR_LENGTH(`markdown`) between 1 and 2000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_detail_jobs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `request_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `items_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `state` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'queued',
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `lease_until` BIGINT NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  `duration_ms` BIGINT NOT NULL DEFAULT 0,
  `reason` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_detail_leases` (
  `lease_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`lease_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_detail_settings` (
  `id` BIGINT NOT NULL,
  `markdown` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_revision` BIGINT NOT NULL DEFAULT 0,
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'openrouter',
  `openai_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `reasoning_effort` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'medium',
  `model_revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CHECK (id=1),
  CHECK (CHAR_LENGTH(markdown)<=10000),
  CHECK (provider IN ('openrouter','openai'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_discovery_runs` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `user_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `company_id` BIGINT NULL,
  `agency_id` BIGINT NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_revision` BIGINT NOT NULL,
  `settings_revision` BIGINT NOT NULL,
  `result_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `created_at` BIGINT NOT NULL,
  `expires_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (json_valid(result_json))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_employer_guesses` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `agency_vacancy_id` BIGINT NOT NULL,
  `position` BIGINT NOT NULL,
  `employer_name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `linked_company_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `matched_vacancy_id` BIGINT NULL,
  `external_vacancy_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CHECK (`position` between 1 and 5)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_extractions` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  `snapshot_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `model_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `config_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_version` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `schema_version` BIGINT NOT NULL,
  `extracted_data` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `processed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `duration_ms` BIGINT NOT NULL,
  `input_tokens` BIGINT NULL,
  `output_tokens` BIGINT NULL,
  `cost_usd` DOUBLE NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `diagnostics` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '{}',
  PRIMARY KEY (`id`),
  CHECK (status IN ('success','error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_import_batches` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `request_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `items_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `state` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'pending',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_import_operations` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NULL,
  `vacancy_id` BIGINT NULL,
  `batch_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `action` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `processed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `input_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `changes_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `message` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_import_write_context` (
  `vacancy_id` BIGINT NOT NULL,
  `generation` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `owner_token` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`vacancy_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_parser_fields` (
  `vacancy_id` BIGINT NOT NULL,
  `field` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `value_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `manual` BIGINT NOT NULL DEFAULT 0,
  `candidate_id` BIGINT NULL,
  PRIMARY KEY (`vacancy_id`,`field`),
  CHECK (manual IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_parser_roles` (
  `vacancy_id` BIGINT NOT NULL,
  `role_id` BIGINT NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  PRIMARY KEY (`vacancy_id`,`role_id`,`candidate_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_publication_keys` (
  `identity_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `vacancy_id` BIGINT NOT NULL,
  PRIMARY KEY (`identity_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_publication_sources` (
  `candidate_id` BIGINT NOT NULL,
  `vacancy_id` BIGINT NOT NULL,
  `url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `canonical_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_namespace` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `external_job_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `extraction_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`candidate_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_raw_snapshot_parts` (
  `snapshot_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `part_index` BIGINT NOT NULL,
  `markdown_raw` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`snapshot_id`,`part_index`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_raw_snapshots` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  `source_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `final_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `markdown_raw` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `content_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `fetched_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `firecrawl_mode` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `firecrawl_credits` DOUBLE NULL,
  `content_status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `diagnostics` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  PRIMARY KEY (`id`),
  CHECK (firecrawl_mode IN ('free','hobby')),
  CHECK (content_status IN ('valid','invalid'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_relevance_profile_state` (
  `user_name` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `profile_version` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `applied_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_role_assignments` (
  `vacancy_id` BIGINT NOT NULL,
  `role_id` BIGINT NOT NULL,
  PRIMARY KEY (`vacancy_id`,`role_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_role_classification` (
  `role_id` BIGINT NOT NULL,
  `markdown` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `parsing_enabled` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`role_id`),
  CHECK (CHAR_LENGTH(markdown)<=10000),
  CHECK (revision>=0),
  CHECK (parsing_enabled IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_role_results` (
  `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NOT NULL,
  `snapshot_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `input_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `tree_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_version` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `config_hash` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `model_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `role_ids` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `status` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `error_code` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `message` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `processed_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `duration_ms` BIGINT NOT NULL,
  `input_tokens` BIGINT NULL,
  `output_tokens` BIGINT NULL,
  `cost_usd` DOUBLE NULL,
  PRIMARY KEY (`id`),
  CHECK (status IN ('success','error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_role_settings` (
  `id` BIGINT NOT NULL,
  `markdown` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `prompt_revision` BIGINT NOT NULL DEFAULT 0,
  `provider` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'openrouter',
  `openai_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `reasoning_effort` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'medium',
  `model_revision` BIGINT NOT NULL DEFAULT 0,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `openrouter_model` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'best_free',
  PRIMARY KEY (`id`),
  CHECK (id=1),
  CHECK (CHAR_LENGTH(markdown)<=5000),
  CHECK (provider IN ('openrouter','openai')),
  CHECK (openrouter_model IN ('best_free','jev'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_roles` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `stable_key` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `name` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `parent_id` BIGINT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `_d1_ascii_nocase_name` LONGTEXT GENERATED ALWAYS AS (REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(`name`,'A','a'),'B','b'),'C','c'),'D','d'),'E','e'),'F','f'),'G','g'),'H','h'),'I','i'),'J','j'),'K','k'),'L','l'),'M','m'),'N','n'),'O','o'),'P','p'),'Q','q'),'R','r'),'S','s'),'T','t'),'U','u'),'V','v'),'W','w'),'X','x'),'Y','y'),'Z','z')) VIRTUAL INVISIBLE,
  PRIMARY KEY (`id`),
  CHECK (CHAR_LENGTH(trim(`name`)) between 1 and 120),
  CHECK (`sort_order` >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_source_parsing` (
  `company_id` BIGINT NULL,
  `agency_id` BIGINT NULL,
  `source_id` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `enabled` BIGINT NOT NULL DEFAULT 0,
  `method` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'scrape',
  `config_json` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '{}',
  `revision` BIGINT NOT NULL DEFAULT 0,
  `last_success_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `last_error` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `auto_enabled` BIGINT NOT NULL DEFAULT 0,
  `interval_days` BIGINT NOT NULL DEFAULT 7,
  `next_run_at` BIGINT NULL,
  `last_attempt_at` BIGINT NULL,
  `last_result` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  CHECK (enabled IN (0,1)),
  CHECK (method='scrape'),
  CHECK (json_valid(config_json)),
  CHECK (auto_enabled IN (0,1)),
  CHECK (interval_days IN (1,3,7)),
  CHECK ((company_id IS NOT NULL)+(agency_id IS NOT NULL)=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE TABLE `vacancy_telegram_sources` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `vacancy_id` BIGINT NOT NULL,
  `channel_id` BIGINT NOT NULL,
  `message_url` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `telegram_contact` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT '',
  `created_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
CREATE UNIQUE INDEX `agency_identity_agency_name_unique` ON `agency_identity` (`agency_name`) USING HASH;
CREATE INDEX `apify_budget_idx` ON `apify_runs` (`account_id`(191),`period_start`(191),`period_end`(191));
CREATE INDEX `candidate_agency_seen_idx` ON `vacancy_candidates` (`agency_id`,`last_seen_at`(191) DESC,`id` DESC);
CREATE INDEX `candidate_attempt_history_idx` ON `vacancy_classification_attempts` (`candidate_id`,`classified_at`(191) DESC,`id`(191) DESC);
CREATE INDEX `candidate_attempt_input_idx` ON `vacancy_classification_attempts` (`candidate_id`,`title_revision`,`title_hash`(191),`model_key`(191));
CREATE INDEX `candidate_canonical_idx` ON `vacancy_candidates` (`canonical_url`(191));
CREATE INDEX `candidate_classification_current_idx` ON `vacancy_classifications` (`candidate_id`,`root_role_id`,`title_revision`,`title_hash`(191),`prompt_version`(191),`model_key`(191),`id` DESC);
CREATE INDEX `candidate_classification_history_idx` ON `vacancy_classifications` (`candidate_id`,`id` DESC);
CREATE INDEX `candidate_classification_role_idx` ON `vacancy_classifications` (`root_role_id`,`candidate_id`);
CREATE INDEX `candidate_company_seen_idx` ON `vacancy_candidates` (`company_id`,`last_seen_at`(191) DESC,`id` DESC);
CREATE INDEX `candidate_external_idx` ON `vacancy_candidates` (`source_namespace`(191),`external_job_id`(191));
CREATE INDEX `candidate_identity_candidate_idx` ON `vacancy_candidate_identities` (`candidate_id`);
CREATE INDEX `candidate_seen_idx` ON `vacancy_candidates` (`last_seen_at`(191) DESC,`id` DESC);
CREATE INDEX `candidate_source_id_seen_idx` ON `vacancy_candidates` (`source_id`(191),`last_seen_at`(191) DESC,`id` DESC);
CREATE INDEX `candidate_source_seen_idx` ON `vacancy_candidates` (`source_namespace`(191),`last_seen_at`(191) DESC,`id` DESC);
CREATE INDEX `classification_job_generation_idx` ON `vacancy_classification_jobs` (`generation`(191),`created_at`);
CREATE INDEX `company_application_status_history_application_idx` ON `company_application_status_history` (`application_id`,`id`);
CREATE INDEX `company_applications_user_company_idx` ON `company_applications` (`user_name`(191),`company_name`(191),`updated_at`(191),`id`);
CREATE UNIQUE INDEX `company_applications_user_vacancy_unique` ON `company_applications` (`user_name`,`vacancy_id`) USING HASH;
CREATE UNIQUE INDEX `company_identity_company_name_unique` ON `company_identity` (`company_name`) USING HASH;
CREATE INDEX `company_status_history_company_idx` ON `company_status_history` (`company_name`(191),`id`);
CREATE INDEX `company_tasks_user_company_idx` ON `company_tasks` (`user_name`(191),`company_name`(191),`completed`,`due_at`(191));
CREATE INDEX `company_tasks_user_due_idx` ON `company_tasks` (`user_name`(191),`completed`,`due_at`(191));
CREATE INDEX `company_tasks_user_vacancy_idx` ON `company_tasks` (`user_name`(191),`vacancy_id`,`completed`,`due_at`(191));
CREATE UNIQUE INDEX `company_user_state_user_company_unique` ON `company_user_state` (`user_name`,`company_name`) USING HASH;
CREATE INDEX `company_user_state_user_favorite_idx` ON `company_user_state` (`user_name`(191),`favorite`,`company_name`(191));
CREATE INDEX `company_user_state_user_hidden_idx` ON `company_user_state` (`user_name`(191),`hidden`,`company_name`(191));
CREATE INDEX `company_vacancies_company_date_idx` ON `company_vacancies` (`company_name`(191),`published_at`(191),`id`);
CREATE INDEX `company_vacancies_entity_idx` ON `company_vacancies` (`entity_type`(191),`company_name`(191),`real_company_name`(191));
CREATE UNIQUE INDEX `company_vacancies_url_unique` ON `company_vacancies` (`url`) USING HASH;
CREATE INDEX `company_vacancy_user_state_user_favorite_idx` ON `company_vacancy_user_state` (`user_name`(191),`favorite`,`vacancy_id`);
CREATE INDEX `company_vacancy_user_state_user_hidden_idx` ON `company_vacancy_user_state` (`user_name`(191),`hidden`,`vacancy_id`);
CREATE UNIQUE INDEX `company_vacancy_user_state_user_vacancy_unique` ON `company_vacancy_user_state` (`user_name`,`vacancy_id`) USING HASH;
CREATE INDEX `content_fetch_candidate_idx` ON `vacancy_content_fetches` (`candidate_id`,`started_at`);
CREATE INDEX `discovery_run_expiry_idx` ON `vacancy_discovery_runs` (`expires_at`);
CREATE INDEX `extraction_current_idx` ON `vacancy_extractions` (`candidate_id`,`snapshot_id`(186),`config_hash`(186),`prompt_version`(186),`schema_version`,`status`(186));
CREATE INDEX `firecrawl_budget_period_idx` ON `firecrawl_usage_events` (`period_start`(187),`period_end`(187),`profile`(187),`key_hash`(187));
CREATE INDEX `firecrawl_rate_idx` ON `firecrawl_usage_events` (`started_at`);
CREATE INDEX `hiring_contacts_company_idx` ON `hiring_contacts` (`company_name`(191),`position`);
CREATE UNIQUE INDEX `hiring_contacts_company_position_unique` ON `hiring_contacts` (`company_name`,`position`);
CREATE INDEX `publication_source_url_idx` ON `vacancy_publication_sources` (`canonical_url`(191));
CREATE INDEX `raw_snapshot_candidate_idx` ON `vacancy_raw_snapshots` (`candidate_id`,`created_at`(191));
CREATE INDEX `role_result_cache_idx` ON `vacancy_role_results` (`candidate_id`,`input_hash`(191),`status`(191),`processed_at`(191));
CREATE UNIQUE INDEX `scheduler_agency_source_active_idx` ON `scheduler_jobs` (`generation`,`agency_id`,`source_id`,`_d1_active_scan`) USING HASH;
CREATE INDEX `scheduler_cost_date_idx` ON `scheduler_attempts` (`started_at`,`type`(191));
CREATE INDEX `scheduler_due_idx` ON `scheduler_jobs` (`generation`(191),`status`(191),`run_after`,`priority`);
CREATE INDEX `scheduler_job_history_idx` ON `scheduler_attempts` (`job_id`(191),`started_at`);
CREATE INDEX `scheduler_runs_date_idx` ON `scheduler_runs` (`started_at` DESC);
CREATE UNIQUE INDEX `scheduler_source_active_idx` ON `scheduler_jobs` (`generation`,`company_id`,`source_id`,`_d1_active_scan`) USING HASH;
CREATE UNIQUE INDEX `telegram_channels_name_unique` ON `telegram_channels` (`name`) USING HASH;
CREATE UNIQUE INDEX `telegram_channels_normalized_username_unique` ON `telegram_channels` (`normalized_username`) USING HASH;
CREATE UNIQUE INDEX `vacancy_cover_letters_user_vacancy_unique` ON `vacancy_cover_letters` (`user_name`,`vacancy_id`) USING HASH;
CREATE INDEX `vacancy_employer_guesses_linked_company_idx` ON `vacancy_employer_guesses` (`linked_company_name`(191));
CREATE INDEX `vacancy_employer_guesses_matched_vacancy_idx` ON `vacancy_employer_guesses` (`matched_vacancy_id`);
CREATE UNIQUE INDEX `vacancy_employer_guesses_vacancy_position_unique` ON `vacancy_employer_guesses` (`agency_vacancy_id`,`position`);
CREATE INDEX `vacancy_import_history_idx` ON `vacancy_import_operations` (`candidate_id`,`processed_at`(191));
CREATE INDEX `vacancy_role_assignments_role_idx` ON `vacancy_role_assignments` (`role_id`,`vacancy_id`);
CREATE UNIQUE INDEX `vacancy_roles_name_unique` ON `vacancy_roles` (`_d1_ascii_nocase_name`) USING HASH;
CREATE INDEX `vacancy_roles_parent_order_idx` ON `vacancy_roles` (`parent_id`,`sort_order`,`id`);
CREATE UNIQUE INDEX `vacancy_roles_stable_key_unique` ON `vacancy_roles` (`stable_key`) USING HASH;
CREATE INDEX `vacancy_telegram_sources_channel_idx` ON `vacancy_telegram_sources` (`channel_id`,`vacancy_id`);
CREATE UNIQUE INDEX `vacancy_telegram_sources_message_url_unique` ON `vacancy_telegram_sources` (`message_url`) USING HASH;
CREATE UNIQUE INDEX `vacancy_telegram_sources_vacancy_channel_unique` ON `vacancy_telegram_sources` (`vacancy_id`,`channel_id`);
CREATE UNIQUE INDEX `d1_unique_0` ON `__appgarden_migrations` (`name`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_1` ON `apify_runs` (`generation`,`operation_key`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_3` ON `scheduler_jobs` (`generation`,`dedupe_key`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_7` ON `vacancy_candidates` (`creation_token`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_1` ON `vacancy_classification_jobs` (`generation`,`request_key`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_3` ON `vacancy_classifications` (`attempt_id`,`root_role_id`);
CREATE UNIQUE INDEX `d1_unique_0` ON `vacancy_detail_jobs` (`generation`,`request_key`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_0` ON `vacancy_import_batches` (`generation`,`request_key`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_0` ON `vacancy_source_parsing` (`company_id`,`source_id`) USING HASH;
CREATE UNIQUE INDEX `d1_unique_1` ON `vacancy_source_parsing` (`agency_id`,`source_id`) USING HASH;
ALTER TABLE `company_application_status_history` ADD CONSTRAINT `d1fk_company_application_status_history_0` FOREIGN KEY (`application_id`) REFERENCES `company_applications` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_applications` ADD CONSTRAINT `d1fk_company_applications_0` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `company_applications` ADD CONSTRAINT `d1fk_company_applications_1` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_status_history` ADD CONSTRAINT `d1fk_company_status_history_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_tasks` ADD CONSTRAINT `d1fk_company_tasks_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_tasks` ADD CONSTRAINT `d1fk_company_tasks_1` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `company_user_state` ADD CONSTRAINT `d1fk_company_user_state_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_vacancies` ADD CONSTRAINT `d1fk_company_vacancies_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_vacancy_state` ADD CONSTRAINT `d1fk_company_vacancy_state_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `company_vacancy_user_state` ADD CONSTRAINT `d1fk_company_vacancy_user_state_0` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `hiring_contacts` ADD CONSTRAINT `d1fk_hiring_contacts_0` FOREIGN KEY (`company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_candidate_classification_leases` ADD CONSTRAINT `d1fk_vacancy_candidate_classification_leases_0` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_candidate_identities` ADD CONSTRAINT `d1fk_vacancy_candidate_identities_0` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_candidates` ADD CONSTRAINT `d1fk_vacancy_candidates_0` FOREIGN KEY (`agency_id`) REFERENCES `agency_identity` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_candidates` ADD CONSTRAINT `d1fk_vacancy_candidates_1` FOREIGN KEY (`main_vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_candidates` ADD CONSTRAINT `d1fk_vacancy_candidates_2` FOREIGN KEY (`company_id`) REFERENCES `company_identity` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_classification_attempts` ADD CONSTRAINT `d1fk_vacancy_classification_attempts_0` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_classifications` ADD CONSTRAINT `d1fk_vacancy_classifications_0` FOREIGN KEY (`root_role_id`) REFERENCES `vacancy_roles` (`id`) ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE `vacancy_classifications` ADD CONSTRAINT `d1fk_vacancy_classifications_1` FOREIGN KEY (`attempt_id`) REFERENCES `vacancy_classification_attempts` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_classifications` ADD CONSTRAINT `d1fk_vacancy_classifications_2` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_content_fetches` ADD CONSTRAINT `d1fk_vacancy_content_fetches_0` FOREIGN KEY (`snapshot_id`) REFERENCES `vacancy_raw_snapshots` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_content_fetches` ADD CONSTRAINT `d1fk_vacancy_content_fetches_1` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_cover_letters` ADD CONSTRAINT `d1fk_vacancy_cover_letters_0` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_employer_guesses` ADD CONSTRAINT `d1fk_vacancy_employer_guesses_0` FOREIGN KEY (`matched_vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_employer_guesses` ADD CONSTRAINT `d1fk_vacancy_employer_guesses_1` FOREIGN KEY (`linked_company_name`) REFERENCES `company_crm` (`company_name`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_employer_guesses` ADD CONSTRAINT `d1fk_vacancy_employer_guesses_2` FOREIGN KEY (`agency_vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_extractions` ADD CONSTRAINT `d1fk_vacancy_extractions_0` FOREIGN KEY (`snapshot_id`) REFERENCES `vacancy_raw_snapshots` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_extractions` ADD CONSTRAINT `d1fk_vacancy_extractions_1` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_import_operations` ADD CONSTRAINT `d1fk_vacancy_import_operations_0` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_import_operations` ADD CONSTRAINT `d1fk_vacancy_import_operations_1` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_parser_fields` ADD CONSTRAINT `d1fk_vacancy_parser_fields_0` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_parser_fields` ADD CONSTRAINT `d1fk_vacancy_parser_fields_1` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_parser_roles` ADD CONSTRAINT `d1fk_vacancy_parser_roles_0` FOREIGN KEY (`vacancy_id`,`role_id`) REFERENCES `vacancy_role_assignments` (`vacancy_id`,`role_id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_parser_roles` ADD CONSTRAINT `d1fk_vacancy_parser_roles_1` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_publication_keys` ADD CONSTRAINT `d1fk_vacancy_publication_keys_0` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_publication_sources` ADD CONSTRAINT `d1fk_vacancy_publication_sources_0` FOREIGN KEY (`extraction_id`) REFERENCES `vacancy_extractions` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE `vacancy_publication_sources` ADD CONSTRAINT `d1fk_vacancy_publication_sources_1` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_publication_sources` ADD CONSTRAINT `d1fk_vacancy_publication_sources_2` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_raw_snapshot_parts` ADD CONSTRAINT `d1fk_vacancy_raw_snapshot_parts_0` FOREIGN KEY (`snapshot_id`) REFERENCES `vacancy_raw_snapshots` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_raw_snapshots` ADD CONSTRAINT `d1fk_vacancy_raw_snapshots_0` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_role_assignments` ADD CONSTRAINT `d1fk_vacancy_role_assignments_0` FOREIGN KEY (`role_id`) REFERENCES `vacancy_roles` (`id`) ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE `vacancy_role_assignments` ADD CONSTRAINT `d1fk_vacancy_role_assignments_1` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_role_classification` ADD CONSTRAINT `d1fk_vacancy_role_classification_0` FOREIGN KEY (`role_id`) REFERENCES `vacancy_roles` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_role_results` ADD CONSTRAINT `d1fk_vacancy_role_results_0` FOREIGN KEY (`snapshot_id`) REFERENCES `vacancy_raw_snapshots` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_role_results` ADD CONSTRAINT `d1fk_vacancy_role_results_1` FOREIGN KEY (`candidate_id`) REFERENCES `vacancy_candidates` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_roles` ADD CONSTRAINT `d1fk_vacancy_roles_0` FOREIGN KEY (`parent_id`) REFERENCES `vacancy_roles` (`id`) ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE `vacancy_source_parsing` ADD CONSTRAINT `d1fk_vacancy_source_parsing_0` FOREIGN KEY (`agency_id`) REFERENCES `agency_identity` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_source_parsing` ADD CONSTRAINT `d1fk_vacancy_source_parsing_1` FOREIGN KEY (`company_id`) REFERENCES `company_identity` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_telegram_sources` ADD CONSTRAINT `d1fk_vacancy_telegram_sources_0` FOREIGN KEY (`channel_id`) REFERENCES `telegram_channels` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
ALTER TABLE `vacancy_telegram_sources` ADD CONSTRAINT `d1fk_vacancy_telegram_sources_1` FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE;
DELIMITER $$
CREATE TRIGGER `company_applications_max_10` BEFORE INSERT ON `company_applications` FOR EACH ROW BEGIN IF (SELECT COUNT(*) FROM `company_applications` WHERE `user_name` = NEW.`user_name` AND `company_name` = NEW.`company_name`) >= 10 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='company application limit reached'; END IF; END$$
CREATE TRIGGER `parser_protect_description_md` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.description_md <=> NEW.description_md) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='description_md'; END IF; END$$
CREATE TRIGGER `parser_protect_level` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.level <=> NEW.level) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='level'; END IF; END$$
CREATE TRIGGER `parser_protect_published_at` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.published_at <=> NEW.published_at) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='published_at'; END IF; END$$
CREATE TRIGGER `parser_protect_salary_from` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.salary_from <=> NEW.salary_from) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='salary_from'; END IF; END$$
CREATE TRIGGER `parser_protect_salary_to` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.salary_to <=> NEW.salary_to) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='salary_to'; END IF; END$$
CREATE TRIGGER `parser_protect_title` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.title <=> NEW.title) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='title'; END IF; END$$
CREATE TRIGGER `parser_protect_url` AFTER UPDATE ON `company_vacancies` FOR EACH ROW BEGIN IF NOT (OLD.url <=> NEW.url) AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) THEN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='url'; END IF; END$$
CREATE TRIGGER `vacancy_employer_guesses_agency_insert` BEFORE INSERT ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`agency_vacancy_id`), '') <> 'agency' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='employer guesses require an agency vacancy'; END IF; END$$
CREATE TRIGGER `vacancy_employer_guesses_agency_update` BEFORE UPDATE ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`agency_vacancy_id`), '') <> 'agency' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='employer guesses require an agency vacancy'; END IF; END$$
CREATE TRIGGER `vacancy_employer_guesses_company_match_insert` BEFORE INSERT ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NEW.`matched_vacancy_id` IS NOT NULL AND COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`matched_vacancy_id`), '') <> 'company' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='matched vacancy must belong to a company'; END IF; END$$
CREATE TRIGGER `vacancy_employer_guesses_company_match_update` BEFORE UPDATE ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NEW.`matched_vacancy_id` IS NOT NULL AND COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`matched_vacancy_id`), '') <> 'company' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='matched vacancy must belong to a company'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_candidates_0_insert` AFTER INSERT ON `vacancy_candidates` FOR EACH ROW BEGIN IF NOT (NEW.`agency_id` IS NULL OR NEW.`company_id` IS NULL) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_candidates_0_update` AFTER UPDATE ON `vacancy_candidates` FOR EACH ROW BEGIN IF NOT (NEW.`agency_id` IS NULL OR NEW.`company_id` IS NULL) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_employer_guesses_1_insert` AFTER INSERT ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NOT (NEW.`matched_vacancy_id` is null or NEW.`matched_vacancy_id` <> NEW.`agency_vacancy_id`) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_employer_guesses_1_update` AFTER UPDATE ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NOT (NEW.`matched_vacancy_id` is null or NEW.`matched_vacancy_id` <> NEW.`agency_vacancy_id`) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_employer_guesses_2_insert` AFTER INSERT ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NOT (CHAR_LENGTH(trim(NEW.`employer_name`)) > 0 or NEW.`linked_company_name` is not null or NEW.`matched_vacancy_id` is not null or NEW.`external_vacancy_url` is not null) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_employer_guesses_2_update` AFTER UPDATE ON `vacancy_employer_guesses` FOR EACH ROW BEGIN IF NOT (CHAR_LENGTH(trim(NEW.`employer_name`)) > 0 or NEW.`linked_company_name` is not null or NEW.`matched_vacancy_id` is not null or NEW.`external_vacancy_url` is not null) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_roles_3_insert` AFTER INSERT ON `vacancy_roles` FOR EACH ROW BEGIN IF NOT (NEW.`parent_id` is null or NEW.`parent_id` <> NEW.`id`) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_check_vacancy_roles_3_update` AFTER UPDATE ON `vacancy_roles` FOR EACH ROW BEGIN IF NOT (NEW.`parent_id` is null or NEW.`parent_id` <> NEW.`id`) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK constraint failed'; END IF; END$$
CREATE TRIGGER `d1_guesses_vacancy_delete` BEFORE DELETE ON `company_vacancies` FOR EACH ROW BEGIN IF EXISTS (SELECT 1 FROM vacancy_employer_guesses g WHERE g.agency_vacancy_id<>OLD.id AND g.matched_vacancy_id=OLD.id AND g.linked_company_name IS NULL AND CHAR_LENGTH(TRIM(g.employer_name))=0) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK: employer guess must retain content'; END IF; END$$
CREATE TRIGGER `d1_guesses_company_delete` BEFORE DELETE ON `company_crm` FOR EACH ROW BEGIN IF EXISTS (SELECT 1 FROM vacancy_employer_guesses g JOIN company_vacancies a ON a.id=g.agency_vacancy_id LEFT JOIN company_vacancies m ON m.id=g.matched_vacancy_id WHERE a.company_name<>OLD.company_name AND CHAR_LENGTH(TRIM(g.employer_name))=0 AND (g.linked_company_name IS NULL OR g.linked_company_name=OLD.company_name) AND (g.matched_vacancy_id IS NULL OR m.company_name=OLD.company_name)) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='SQLite CHECK: employer guess must retain content'; END IF; END$$
DELIMITER ;
SET SESSION sql_mode=@d1_old_sql_mode; SET SESSION time_zone=@d1_old_time_zone;
