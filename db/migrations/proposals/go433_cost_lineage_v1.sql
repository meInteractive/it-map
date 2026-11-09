-- GO-433 / PROPOSAL ONLY. NOT part of the trusted v1 baseline.
-- Never execute against production/staging or during normal HTTP startup.
-- Only the disposable, nonce-guarded MariaDB integration test loads this draft.
-- No ALTER to v1 tables. No data backfill or source/provider HTTP calls.
CREATE TABLE `cost_scan_cohorts` (
  `cohort_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `generation` VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `scan_job_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_revision` BIGINT NOT NULL,
  `company_id` BIGINT NULL,
  `agency_id` BIGINT NULL,
  `origin` VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `started_at_ms` BIGINT NOT NULL,
  PRIMARY KEY (`cohort_id`),
  UNIQUE KEY `ux_cost_cohort_scan_job` (`generation`,`scan_job_id`),
  CHECK (CHAR_LENGTH(`generation`)=36),
  CHECK (`source_revision`>=0 AND `started_at_ms`>0),
  CHECK (`company_id` IS NULL OR `company_id`>0),
  CHECK (`agency_id` IS NULL OR `agency_id`>0),
  CHECK (`origin` IN ('new_scan','reuse_existing_actor'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE `cost_provider_operations` (
  `operation_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `provider_kind` VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_ledger` VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source_receipt_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `billing_scope` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `provider_operation_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NULL,
  `created_at_ms` BIGINT NOT NULL,
  PRIMARY KEY (`operation_id`),
  UNIQUE KEY `ux_cost_local_receipt` (`source_ledger`,`source_receipt_id`),
  UNIQUE KEY `ux_cost_provider_identity` (`provider_kind`,`billing_scope`,`provider_operation_id`),
  CHECK (`provider_kind` IN ('apify_actor','apify_dataset_read','ai_provider_request','firecrawl_request','ai_attempt')),
  CHECK (`source_ledger` IN ('apify_runs','scheduler_attempts','provider_receipts','firecrawl_requests','apify_dataset_reads')),
  CHECK (`created_at_ms`>0),
  CHECK ((`billing_scope` IS NULL AND `provider_operation_id` IS NULL) OR
         (`billing_scope` IS NOT NULL AND `provider_operation_id` IS NOT NULL)),
  CHECK (`provider_kind`<>'ai_attempt' OR `provider_operation_id` IS NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE `cost_operation_settlements` (
  `settlement_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `operation_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `settlement_revision` BIGINT NOT NULL,
  `status` VARCHAR(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `source` VARCHAR(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `cost_usd` DECIMAL(20,8) NULL,
  `reserved_usd` DECIMAL(20,8) NULL,
  `credits` DECIMAL(20,8) NULL,
  `provider_verified` TINYINT NOT NULL DEFAULT 0,
  `observed_at_ms` BIGINT NOT NULL,
  PRIMARY KEY (`settlement_id`),
  UNIQUE KEY `ux_cost_settlement_revision` (`operation_id`,`settlement_revision`),
  CONSTRAINT `fk_cost_settlement_operation` FOREIGN KEY (`operation_id`)
    REFERENCES `cost_provider_operations`(`operation_id`) ON DELETE RESTRICT,
  CHECK (`settlement_revision`>0 AND `observed_at_ms`>0),
  CHECK (`status` IN ('unknown','settled','disputed')),
  CHECK (`source` IN ('local','provider')),
  CHECK (`cost_usd` IS NULL OR `cost_usd`>=0),
  CHECK (`reserved_usd` IS NULL OR `reserved_usd`>=0),
  CHECK (`credits` IS NULL OR `credits`>=0),
  CHECK (`provider_verified` IN (0,1)),
  CHECK (`provider_verified`=0 OR (`source`='provider' AND `status`='settled' AND `cost_usd` IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE `cost_operation_cohort_links` (
  `operation_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `cohort_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `relation` VARCHAR(24) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `captured` TINYINT NOT NULL DEFAULT 0,
  `captured_at_ms` BIGINT NOT NULL,
  PRIMARY KEY (`operation_id`,`cohort_id`,`relation`),
  KEY `ix_cost_cohort_links` (`cohort_id`,`operation_id`),
  CONSTRAINT `fk_cost_link_operation` FOREIGN KEY (`operation_id`)
    REFERENCES `cost_provider_operations`(`operation_id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_cost_link_cohort` FOREIGN KEY (`cohort_id`)
    REFERENCES `cost_scan_cohorts`(`cohort_id`) ON DELETE RESTRICT,
  CHECK (`relation` IN ('initiated_by','reused_in','observed_from')),
  CHECK (`captured` IN (0,1)),
  CHECK (`captured_at_ms`>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE `cost_discovery_events` (
  `event_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `cohort_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `operation_id` VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `relation` VARCHAR(24) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL DEFAULT 'observed_from',
  `dataset_item_key` VARCHAR(96) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `candidate_id` BIGINT NULL,
  `result` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin NOT NULL,
  `captured_at_ms` BIGINT NOT NULL,
  PRIMARY KEY (`event_id`),
  UNIQUE KEY `ux_cost_discovery_item` (`cohort_id`,`operation_id`,`dataset_item_key`),
  KEY `ix_cost_discovery_candidate` (`candidate_id`),
  CONSTRAINT `fk_cost_discovery_link` FOREIGN KEY (`operation_id`,`cohort_id`,`relation`)
    REFERENCES `cost_operation_cohort_links`(`operation_id`,`cohort_id`,`relation`) ON DELETE RESTRICT,
  CHECK (`relation`='observed_from'),
  CHECK (`result` IN ('created','existing','rejected')),
  CHECK (`candidate_id` IS NULL OR `candidate_id`>0),
  CHECK (`captured_at_ms`>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_nopad_bin ROW_FORMAT=DYNAMIC;
