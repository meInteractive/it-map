import { sql } from "drizzle-orm";
import { check, index, integer, real, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

export const companyCrm = sqliteTable("company_crm", {
  companyName: text("company_name").primaryKey(),
  status: text("status").notNull().default(""),
  commentMd: text("comment_md").notNull().default(""),
  favorite: integer("favorite", { mode: "boolean" }).notNull().default(false),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const companyStatusHistory = sqliteTable(
  "company_status_history",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    userName: text("user_name").notNull().default("Eugene"),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    fromStatus: text("from_status").notNull().default(""),
    toStatus: text("to_status").notNull().default(""),
    changedAt: text("changed_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [index("company_status_history_company_idx").on(table.companyName, table.id)],
);

export const companyTasks = sqliteTable(
  "company_tasks",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    userName: text("user_name").notNull().default("Eugene"),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    vacancyId: integer("vacancy_id").references(() => companyVacancies.id, { onDelete: "set null" }),
    type: text("type").notNull(),
    note: text("note").notNull().default(""),
    dueAt: text("due_at").notNull(),
    completed: integer("completed", { mode: "boolean" }).notNull().default(false),
    googleEventId: text("google_event_id"),
    googleEventHtmlLink: text("google_event_html_link"),
    googleSyncStatus: text("google_sync_status").notNull().default(""),
    googleSyncError: text("google_sync_error").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    completedAt: text("completed_at"),
  },
  (table) => [
    index("company_tasks_user_due_idx").on(table.userName, table.completed, table.dueAt),
    index("company_tasks_user_company_idx").on(table.userName, table.companyName, table.completed, table.dueAt),
    index("company_tasks_user_vacancy_idx").on(table.userName, table.vacancyId, table.completed, table.dueAt),
  ],
);

export const googleCalendarConnection = sqliteTable("google_calendar_connection", {
  id: integer("id").primaryKey(),
  accountEmail: text("account_email").notNull().default(""),
  refreshTokenEncrypted: text("refresh_token_encrypted").notNull(),
  calendarId: text("calendar_id").notNull().default("primary"),
  connectedAt: text("connected_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const googleCalendarUserConnection = sqliteTable("google_calendar_user_connection", {
  userName: text("user_name").primaryKey(),
  accountEmail: text("account_email").notNull().default(""),
  refreshTokenEncrypted: text("refresh_token_encrypted").notNull(),
  calendarId: text("calendar_id").notNull().default("primary"),
  connectedAt: text("connected_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const companyVacancies = sqliteTable(
  "company_vacancies",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    entityType: text("entity_type").notNull().default("company"),
    realCompanyName: text("real_company_name"),
    url: text("url").notNull(),
    salaryFrom: integer("salary_from"),
    salaryTo: integer("salary_to"),
    title: text("title").notNull(),
    role: text("role").notNull().default(""),
    level: text("level").notNull().default(""),
    relevance: integer("relevance"),
    publishedAt: text("published_at"),
    loadedAt: text("loaded_at"),
    lastCheckedAt: text("last_checked_at"),
    archived: integer("archived", { mode: "boolean" }).notNull().default(false),
    descriptionMd: text("description_md").notNull().default(""),
    coverLetterSubject: text("cover_letter_subject").notNull().default(""),
    coverLetterMd: text("cover_letter_md").notNull().default(""),
    applicationStatus: text("application_status").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    index("company_vacancies_company_date_idx").on(table.companyName, table.publishedAt, table.id),
    index("company_vacancies_entity_idx").on(table.entityType, table.companyName, table.realCompanyName),
    uniqueIndex("company_vacancies_url_unique").on(table.url),
    check("company_vacancies_relevance_check", sql`${table.relevance} is null or (${table.relevance} between 0 and 100)`),
  ],
);

export const vacancyEmployerGuesses = sqliteTable(
  "vacancy_employer_guesses",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    agencyVacancyId: integer("agency_vacancy_id")
      .notNull()
      .references(() => companyVacancies.id, { onDelete: "cascade" }),
    position: integer("position").notNull(),
    employerName: text("employer_name").notNull().default(""),
    linkedCompanyName: text("linked_company_name").references(() => companyCrm.companyName, { onDelete: "set null" }),
    matchedVacancyId: integer("matched_vacancy_id").references(() => companyVacancies.id, { onDelete: "set null" }),
    externalVacancyUrl: text("external_vacancy_url"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("vacancy_employer_guesses_vacancy_position_unique").on(table.agencyVacancyId, table.position),
    index("vacancy_employer_guesses_linked_company_idx").on(table.linkedCompanyName),
    index("vacancy_employer_guesses_matched_vacancy_idx").on(table.matchedVacancyId),
    check("vacancy_employer_guesses_position_check", sql`${table.position} between 1 and 5`),
    check("vacancy_employer_guesses_not_self_check", sql`${table.matchedVacancyId} is null or ${table.matchedVacancyId} <> ${table.agencyVacancyId}`),
    check(
      "vacancy_employer_guesses_content_check",
      sql`length(trim(${table.employerName})) > 0 or ${table.linkedCompanyName} is not null or ${table.matchedVacancyId} is not null or ${table.externalVacancyUrl} is not null`,
    ),
  ],
);

export const hiringContacts = sqliteTable(
  "hiring_contacts",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    position: integer("position").notNull(),
    fullName: text("full_name").notNull(),
    linkedinUrl: text("linkedin_url").notNull().default(""),
    facebookUrl: text("facebook_url").notNull().default(""),
    setkaUrl: text("setka_url").notNull().default(""),
    telegram: text("telegram").notNull().default(""),
    phone: text("phone").notNull().default(""),
    email: text("email").notNull().default(""),
    specialization: text("specialization").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("hiring_contacts_company_position_unique").on(table.companyName, table.position),
    index("hiring_contacts_company_idx").on(table.companyName, table.position),
    check("hiring_contacts_position_check", sql`${table.position} between 1 and 3`),
  ],
);

export const companyVacancyState = sqliteTable("company_vacancy_state", {
  companyName: text("company_name")
    .primaryKey()
    .references(() => companyCrm.companyName, { onDelete: "cascade" }),
  initializedAt: text("initialized_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const companyUserState = sqliteTable(
  "company_user_state",
  {
    userName: text("user_name").notNull(),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    status: text("status").notNull().default(""),
    commentMd: text("comment_md").notNull().default(""),
    favorite: integer("favorite", { mode: "boolean" }).notNull().default(false),
    hidden: integer("hidden", { mode: "boolean" }).notNull().default(false),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("company_user_state_user_company_unique").on(table.userName, table.companyName),
    index("company_user_state_user_favorite_idx").on(table.userName, table.favorite, table.companyName),
    index("company_user_state_user_hidden_idx").on(table.userName, table.hidden, table.companyName),
  ],
);

export const companyVacancyUserState = sqliteTable(
  "company_vacancy_user_state",
  {
    userName: text("user_name").notNull(),
    vacancyId: integer("vacancy_id")
      .notNull()
      .references(() => companyVacancies.id, { onDelete: "cascade" }),
    relevance: integer("relevance"),
    favorite: integer("favorite", { mode: "boolean" }).notNull().default(false),
    hidden: integer("hidden", { mode: "boolean" }).notNull().default(false),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("company_vacancy_user_state_user_vacancy_unique").on(table.userName, table.vacancyId),
    index("company_vacancy_user_state_user_favorite_idx").on(table.userName, table.favorite, table.vacancyId),
    index("company_vacancy_user_state_user_hidden_idx").on(table.userName, table.hidden, table.vacancyId),
    check("company_vacancy_user_state_relevance_check", sql`${table.relevance} is null or (${table.relevance} between 0 and 100)`),
  ],
);

export const vacancyCoverLetters = sqliteTable("vacancy_cover_letters", {
  userName: text("user_name").notNull(),
  vacancyId: integer("vacancy_id").notNull().references(() => companyVacancies.id, { onDelete: "cascade" }),
  subject: text("subject").notNull(),
  markdown: text("markdown").notNull(),
  overrideShared: integer("override_shared").notNull().default(0),
  revision: integer("revision").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, (table) => [
  uniqueIndex("vacancy_cover_letters_user_vacancy_unique").on(table.userName, table.vacancyId),
  check("vacancy_cover_letters_subject_length", sql`length(${table.subject}) between 1 and 512`),
  check("vacancy_cover_letters_markdown_length", sql`length(${table.markdown}) between 1 and 2000`),
  check("vacancy_cover_letters_override_shared", sql`${table.overrideShared} in (0, 1)`),
  check("vacancy_cover_letters_revision", sql`${table.revision} >= 0`),
]);

export const vacancyRelevanceProfileState = sqliteTable("vacancy_relevance_profile_state", {
  userName: text("user_name").primaryKey(),
  profileVersion: text("profile_version").notNull(),
  appliedAt: text("applied_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const companyApplications = sqliteTable(
  "company_applications",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    userName: text("user_name").notNull().default("Eugene"),
    companyName: text("company_name")
      .notNull()
      .references(() => companyCrm.companyName, { onDelete: "cascade" }),
    vacancyId: integer("vacancy_id").references(() => companyVacancies.id, { onDelete: "set null" }),
    status: text("status").notNull().default("Откликнулся"),
    commentMd: text("comment_md").notNull().default(""),
    recruiterName: text("recruiter_name").notNull().default(""),
    recruiterPhone: text("recruiter_phone").notNull().default(""),
    recruiterEmail: text("recruiter_email").notNull().default(""),
    recruiterContactType: text("recruiter_contact_type").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    index("company_applications_user_company_idx").on(table.userName, table.companyName, table.updatedAt, table.id),
    uniqueIndex("company_applications_user_vacancy_unique").on(table.userName, table.vacancyId),
  ],
);

export const companyApplicationStatusHistory = sqliteTable(
  "company_application_status_history",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    applicationId: integer("application_id")
      .notNull()
      .references(() => companyApplications.id, { onDelete: "cascade" }),
    fromStatus: text("from_status").notNull().default(""),
    toStatus: text("to_status").notNull().default(""),
    changedAt: text("changed_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [index("company_application_status_history_application_idx").on(table.applicationId, table.id)],
);

export const appCatalog = sqliteTable("app_catalog", {
  id: integer("id").primaryKey(),
  dataJson: text("data_json").notNull(),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const companyIdentity = sqliteTable("company_identity", {
  id: integer("id").primaryKey({ autoIncrement: true }),
  companyName: text("company_name").notNull().unique(),
});

export const agencyIdentity = sqliteTable("agency_identity", {
  id: integer("id").primaryKey({ autoIncrement: true }),
  agencyName: text("agency_name").notNull().unique(),
});

export const telegramChannels = sqliteTable(
  "telegram_channels",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    name: text("name").notNull().unique(),
    url: text("url").notNull(),
    normalizedUsername: text("normalized_username").notNull().unique(),
    username: text("username").notNull().default(""),
    lastSuccessfulCheckAt: text("last_successful_check_at"),
    comment: text("comment").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    check("telegram_channels_comment_length_check", sql`length(${table.comment}) <= 512`),
  ],
);

export const vacancyTelegramSources = sqliteTable(
  "vacancy_telegram_sources",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    vacancyId: integer("vacancy_id")
      .notNull()
      .references(() => companyVacancies.id, { onDelete: "cascade" }),
    channelId: integer("channel_id")
      .notNull()
      .references(() => telegramChannels.id, { onDelete: "cascade" }),
    messageUrl: text("message_url").notNull().unique(),
    telegramContact: text("telegram_contact").notNull().default(""),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("vacancy_telegram_sources_vacancy_channel_unique").on(table.vacancyId, table.channelId),
    index("vacancy_telegram_sources_channel_idx").on(table.channelId, table.vacancyId),
  ],
);

// Historical audit records are application data and must be portable.
export const vacancyAuditRuns = sqliteTable("vacancy_audit_runs", {
  runId: text("run_id").primaryKey().notNull(),
  deletedCount: integer("deleted_count").notNull(),
  publicationDateUpdateCount: integer("publication_date_update_count").notNull(),
  completedAt: text("completed_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

// Private candidate profile; user is always selected from the authenticated session.
export const userProfiles = sqliteTable("user_profiles", {
  userName: text("user_name").primaryKey().notNull(),
  resumeMd: text("resume_md").notNull().default(""),
  revision: integer("revision").notNull().default(0),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, (table) => [
  check("user_profiles_resume_length", sql`length(${table.resumeMd}) <= 20000`),
  check("user_profiles_revision", sql`${table.revision} >= 0`),
]);

export const systemPrompts = sqliteTable("system_prompts", {
  kind: text("kind").primaryKey().notNull(),
  markdown: text("markdown").notNull().default(""),
  revision: integer("revision").notNull().default(0),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, (table) => [
  check("system_prompts_kind", sql`${table.kind} IN ('cover_letter','relevance')`),
  check("system_prompts_length", sql`length(${table.markdown}) <= CASE WHEN ${table.kind} = 'cover_letter' THEN 15000 ELSE 5000 END`),
  check("system_prompts_revision", sql`${table.revision} >= 0`),
]);

export const openrouterSettings = sqliteTable("openrouter_settings", {
 id: integer("id").primaryKey().notNull(),
 keyEncrypted: text("key_encrypted").notNull().default(""),
 keySuffix: text("key_suffix").notNull().default(""),
 connectionStatus: text("connection_status").notNull().default("missing"),
 lastError: text("last_error").notNull().default(""),
 cacheJson: text("cache_json").notNull().default("[]"),
 primaryModelId: text("primary_model_id"),
 modelsSyncedAt: text("models_synced_at"),
 lastUsedModel: text("last_used_model"),
 lastUsedAt: text("last_used_at"),
 revision: integer("revision").notNull().default(0),
});
export const openrouterAiLogs = sqliteTable("openrouter_ai_logs", {
 id: integer("id").primaryKey({autoIncrement:true}).notNull(),
 createdAt: text("created_at").notNull(), task: text("task").notNull(),
 requestedPrimaryModel: text("requested_primary_model"), actualModel: text("actual_model"),
 fallbackUsed: integer("fallback_used").notNull().default(0), success: integer("success").notNull(), errorCode: text("error_code"),
});
export const openrouterModelMetrics = sqliteTable("openrouter_model_metrics", {
 modelId: text("model_id").primaryKey().notNull(),
 successCount: integer("success_count").notNull().default(0),
 totalDurationMs: integer("total_duration_ms").notNull().default(0),
 lastDurationMs: integer("last_duration_ms"),
 lastSuccessAt: text("last_success_at"),
 failureCount: integer("failure_count").notNull().default(0),
 lastFailureDurationMs: integer("last_failure_duration_ms"),
 lastFailureAt: text("last_failure_at"),
});

// Operational state is never restored from a user backup.
export const appRuntimeGeneration = sqliteTable("app_runtime_generation", {
 id: integer("id").primaryKey().notNull(),
 generation: text("generation").notNull(),
 updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, table => [
 check("app_runtime_generation_singleton", sql`${table.id} = 1`),
 check("app_runtime_generation_length", sql`length(${table.generation}) = 36`),
]);

import { primaryKey } from "drizzle-orm/sqlite-core";
// Receipts survive deletion/restore and are excluded from user backup payloads.
export const applicationCreationReceipts = sqliteTable("application_creation_receipts", {
 userName: text("user_name").notNull(),
 idempotencyKey: text("idempotency_key").notNull(),
 generation: text("generation").notNull(),
 requestHash: text("request_hash").notNull(),
 createdApplicationId: integer("created_application_id"),
 createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, table => [
 primaryKey({columns:[table.userName, table.idempotencyKey]}),
 check("application_receipt_user", sql`length(${table.userName}) BETWEEN 1 AND 160`),
 check("application_receipt_key", sql`length(${table.idempotencyKey}) = 36`),
 check("application_receipt_generation", sql`length(${table.generation}) = 36`),
 check("application_receipt_hash", sql`length(${table.requestHash}) = 64 AND ${table.requestHash} NOT GLOB '*[^0-9a-f]*'`),
 check("application_receipt_id", sql`${table.createdApplicationId} IS NULL OR ${table.createdApplicationId} > 0`),
]);

// Private operational coordination, excluded from user backup/restore; no FK/cascade.
export const coverLetterGenerationLeases = sqliteTable("cover_letter_generation_leases", {
 userName: text("user_name").notNull(),
 vacancyId: integer("vacancy_id").notNull(),
 generation: text("generation").notNull(),
 inputHash: text("input_hash").notNull(),
 ownerToken: text("owner_token").notNull(),
 expiresAt: integer("expires_at").notNull(),
}, table => [
 primaryKey({columns:[table.userName, table.vacancyId]}),
 check("letter_lease_user", sql`length(${table.userName}) BETWEEN 1 AND 160`),
 check("letter_lease_vacancy", sql`${table.vacancyId} > 0`),
 check("letter_lease_generation", sql`length(${table.generation}) = 36`),
 check("letter_lease_hash", sql`length(${table.inputHash}) = 64 AND ${table.inputHash} NOT GLOB '*[^0-9a-f]*'`),
 check("letter_lease_owner", sql`length(${table.ownerToken}) = 36`),
 check("letter_lease_expiry", sql`${table.expiresAt} > 0`),
]);

// Global per-prompt provider choices; no API keys or candidate-specific values.
export const promptModelSettings = sqliteTable("prompt_model_settings", {
 kind: text("kind").primaryKey().notNull(),
 provider: text("provider").notNull().default("openrouter"),
 openaiModel: text("openai_model").notNull().default(""),
 openrouterModel: text("openrouter_model").notNull().default("best_free"),
 reasoningEffort: text("reasoning_effort").notNull().default("medium"),
 revision: integer("revision").notNull().default(0),
 updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, table => [
 check("prompt_model_kind", sql`${table.kind} IN ('cover_letter','relevance')`),
 check("prompt_model_provider", sql`${table.provider} IN ('openrouter','openai')`),
 check("prompt_model_openrouter", sql`${table.openrouterModel} IN ('best_free','jev') AND (${table.kind} = 'relevance' OR ${table.openrouterModel} = 'best_free')`),
 check("prompt_model_id", sql`length(${table.openaiModel}) <= 160`),
 check("prompt_model_effort", sql`${table.reasoningEffort} IN ('auto','none','minimal','low','medium','high','xhigh','max')`),
 check("prompt_model_revision", sql`${table.revision} >= 0`),
]);

// Admin-managed OpenAI key, encrypted with the server secret and a provider domain.
export const openaiSettings = sqliteTable("openai_settings", {
 id: integer("id").primaryKey().notNull(),
 keyEncrypted: text("key_encrypted").notNull().default(""),
 keySuffix: text("key_suffix").notNull().default(""),
 revision: integer("revision").notNull().default(0),
 updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, table => [
 check("openai_settings_singleton", sql`${table.id} = 1`),
 check("openai_settings_revision", sql`${table.revision} >= 0`),
]);

export const vacancyRoles = sqliteTable(
  "vacancy_roles",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    stableKey: text("stable_key").notNull(),
    name: text("name").notNull(),
    // The append-only SQL migration carries the recursive foreign key.
    parentId: integer("parent_id"),
    sortOrder: integer("sort_order").notNull().default(0),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("vacancy_roles_stable_key_unique").on(table.stableKey),
    uniqueIndex("vacancy_roles_name_unique").on(table.name),
    index("vacancy_roles_parent_order_idx").on(table.parentId, table.sortOrder, table.id),
    check("vacancy_roles_name_length", sql`length(trim(${table.name})) between 1 and 120`),
    check("vacancy_roles_sort_order", sql`${table.sortOrder} >= 0`),
    check("vacancy_roles_not_self_parent", sql`${table.parentId} is null or ${table.parentId} <> ${table.id}`),
  ],
);

export const vacancyRoleAssignments = sqliteTable(
  "vacancy_role_assignments",
  {
    vacancyId: integer("vacancy_id").notNull().references(() => companyVacancies.id, { onDelete: "cascade" }),
    roleId: integer("role_id").notNull().references(() => vacancyRoles.id, { onDelete: "restrict" }),
  },
  (table) => [
    uniqueIndex("vacancy_role_assignments_unique").on(table.vacancyId, table.roleId),
    index("vacancy_role_assignments_role_idx").on(table.roleId, table.vacancyId),
  ],
);

export const vacancyRoleClassification = sqliteTable("vacancy_role_classification", {
  roleId: integer("role_id").primaryKey().references(() => vacancyRoles.id, {onDelete: "cascade"}),
  markdown: text("markdown").notNull().default(""),
  revision: integer("revision").notNull().default(0),
  parsingEnabled: integer("parsing_enabled").notNull().default(0),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
}, table => [
  check("role_classification_length", sql`length(${table.markdown}) <= 10000`),
  check("role_classification_revision", sql`${table.revision} >= 0`),
  check("role_classification_enabled", sql`${table.parsingEnabled} in (0,1)`),
]);

export const vacancyCandidates = sqliteTable("vacancy_candidates", {
 id: integer("id").primaryKey({autoIncrement:true}),
 creationToken: text("creation_token").notNull().unique(),
 companyId: integer("company_id").references(()=>companyIdentity.id,{onDelete:"set null"}),
 agencyId: integer("agency_id").references(()=>agencyIdentity.id,{onDelete:"set null"}),
 companyNameRaw: text("company_name_raw").notNull().default(""),
 titleRaw: text("title_raw").notNull(),titleHash:text("title_hash").notNull(),titleRevision:integer("title_revision").notNull().default(1),
 vacancyUrl:text("vacancy_url"),canonicalUrl:text("canonical_url"),sourceUrl:text("source_url"),sourceId:text("source_id"),sourceNamespace:text("source_namespace"),externalJobId:text("external_job_id"),importKey:text("import_key"),
 acquisitionMethod:text("acquisition_method").notNull(),firstSeenAt:text("first_seen_at").notNull(),lastSeenAt:text("last_seen_at").notNull(),titleUpdatedAt:text("title_updated_at"),
 mainVacancyId:integer("main_vacancy_id").references(()=>companyVacancies.id,{onDelete:"set null"}),createdAt:text("created_at").notNull(),updatedAt:text("updated_at").notNull(),
},t=>[index("candidate_company_seen_idx").on(t.companyId,t.lastSeenAt,t.id),index("candidate_seen_idx").on(t.lastSeenAt,t.id),index("candidate_source_seen_idx").on(t.sourceNamespace,t.lastSeenAt,t.id),index("candidate_source_id_seen_idx").on(t.sourceId,t.lastSeenAt,t.id),index("candidate_external_idx").on(t.sourceNamespace,t.externalJobId),index("candidate_canonical_idx").on(t.canonicalUrl)]);
export const vacancyCandidateIdentities = sqliteTable("vacancy_candidate_identities", {
 identityKey:text("identity_key").primaryKey(),candidateId:integer("candidate_id").notNull().references(()=>vacancyCandidates.id,{onDelete:"cascade"}),
},t=>[index("candidate_identity_candidate_idx").on(t.candidateId)]);
export const vacancyClassificationAttempts = sqliteTable("vacancy_classification_attempts", {
 id:text("id").primaryKey(),candidateId:integer("candidate_id").notNull().references(()=>vacancyCandidates.id,{onDelete:"cascade"}),
 titleRaw:text("title_raw").notNull(),titleHash:text("title_hash").notNull(),titleRevision:integer("title_revision").notNull(),modelKey:text("model_key").notNull(),modelId:text("model_id"),requestedRolesJson:text("requested_roles_json").notNull(),errorsJson:text("errors_json").notNull().default("[]"),outcome:text("outcome").notNull(),classifiedAt:text("classified_at").notNull(),durationMs:integer("duration_ms"),costUsd:real("cost_usd"),
},t=>[index("candidate_attempt_history_idx").on(t.candidateId,t.classifiedAt,t.id),index("candidate_attempt_input_idx").on(t.candidateId,t.titleRevision,t.titleHash,t.modelKey)]);
export const vacancyClassifications = sqliteTable("vacancy_classifications", {
 id:integer("id").primaryKey({autoIncrement:true}),candidateId:integer("candidate_id").notNull().references(()=>vacancyCandidates.id,{onDelete:"cascade"}),attemptId:text("attempt_id").notNull().references(()=>vacancyClassificationAttempts.id,{onDelete:"cascade"}),rootRoleId:integer("root_role_id").notNull().references(()=>vacancyRoles.id,{onDelete:"restrict"}),rootRoleName:text("root_role_name").notNull(),decision:text("decision").notNull(),modelKey:text("model_key").notNull(),modelId:text("model_id").notNull(),promptVersion:text("prompt_version").notNull(),promptRevision:integer("prompt_revision").notNull(),titleHash:text("title_hash").notNull(),titleRevision:integer("title_revision").notNull(),classifiedAt:text("classified_at").notNull(),
},t=>[uniqueIndex("classification_attempt_role_unique").on(t.attemptId,t.rootRoleId),index("candidate_classification_current_idx").on(t.candidateId,t.rootRoleId,t.titleRevision,t.titleHash,t.promptVersion,t.modelKey,t.id),index("candidate_classification_history_idx").on(t.candidateId,t.id),index("candidate_classification_role_idx").on(t.rootRoleId,t.candidateId)]);
export const vacancyCandidateClassificationLeases = sqliteTable("vacancy_candidate_classification_leases", {
 candidateId:integer("candidate_id").primaryKey().references(()=>vacancyCandidates.id,{onDelete:"cascade"}),generation:text("generation").notNull(),ownerToken:text("owner_token").notNull(),expiresAt:integer("expires_at").notNull(),
});

export const firecrawlSettings = sqliteTable('firecrawl_settings',{
 schedulerJson:text('scheduler_json').notNull().default("{\"enabled\":false,\"ownerName\":\"Eugene\",\"dayBudgetUsd\":0,\"monthBudgetUsd\":0,\"revision\":0,\"cloudOwnerId\":\"\",\"priorities\":{\"import_vacancy\":100,\"assign_roles\":90,\"extract_vacancy\":80,\"fetch_full_text\":70,\"classify_title\":60,\"scan_source\":20,\"diagnostic\":0}}"),
 favoritesOnly:integer('favorites_only').notNull().default(1),
 id:integer('id').primaryKey(),keyEncrypted:text('key_encrypted').notNull().default(''),keySuffix:text('key_suffix').notNull().default(''),enabled:integer('enabled').notNull().default(0),maxPages:integer('max_pages').notNull().default(3),concurrency:integer('concurrency').notNull().default(1),timeoutMs:integer('timeout_ms').notNull().default(45000),proxy:text('proxy').notNull().default('basic'),enhancedAllowed:integer('enhanced_allowed').notNull().default(0),revision:integer('revision').notNull().default(0),connectionStatus:text('connection_status').notNull().default('missing'),lastError:text('last_error').notNull().default(''),checkedAt:text('checked_at'),
 mode:text('mode').notNull().default('free'),hobbyKeyEncrypted:text('hobby_key_encrypted').notNull().default(''),hobbyKeySuffix:text('hobby_key_suffix').notNull().default(''),hobbyConnectionStatus:text('hobby_connection_status').notNull().default('missing'),hobbyLastError:text('hobby_last_error').notNull().default(''),hobbyCheckedAt:text('hobby_checked_at'),freeKeyHash:text('free_key_hash').notNull().default(''),hobbyKeyHash:text('hobby_key_hash').notNull().default(''),freeUsageJson:text('free_usage_json').notNull().default(''),hobbyUsageJson:text('hobby_usage_json').notNull().default(''),
});
export const vacancySourceParsing = sqliteTable('vacancy_source_parsing',{
 autoEnabled:integer('auto_enabled').notNull().default(0),intervalDays:integer('interval_days').notNull().default(7),nextRunAt:integer('next_run_at'),lastAttemptAt:integer('last_attempt_at'),lastResult:text('last_result').notNull().default(''),
 companyId:integer('company_id').references(()=>companyIdentity.id,{onDelete:'cascade'}),agencyId:integer('agency_id').references(()=>agencyIdentity.id,{onDelete:'cascade'}),sourceId:text('source_id').notNull(),sourceUrl:text('source_url').notNull(),enabled:integer('enabled').notNull().default(0),method:text('method').notNull().default('scrape'),configJson:text('config_json').notNull().default('{}'),revision:integer('revision').notNull().default(0),lastSuccessAt:text('last_success_at'),lastError:text('last_error').notNull().default(''),
},t=>[uniqueIndex('source_parsing_identity').on(t.companyId,t.sourceId),uniqueIndex('source_parsing_agency_identity').on(t.agencyId,t.sourceId),check('source_exact_owner',sql`(${t.companyId} IS NOT NULL)+(${t.agencyId} IS NOT NULL)=1`)]);
export const vacancyDiscoveryRuns = sqliteTable('vacancy_discovery_runs',{
 id:text('id').primaryKey(),userName:text('user_name').notNull(),generation:text('generation').notNull(),companyId:integer('company_id'),agencyId:integer('agency_id'),sourceId:text('source_id').notNull(),sourceUrl:text('source_url').notNull(),sourceRevision:integer('source_revision').notNull(),settingsRevision:integer('settings_revision').notNull(),resultJson:text('result_json').notNull(),createdAt:integer('created_at').notNull(),expiresAt:integer('expires_at').notNull(),
},t=>[index('discovery_run_expiry_idx').on(t.expiresAt)]);
export const firecrawlRequestLease = sqliteTable('firecrawl_request_lease',{
 id:integer('id').primaryKey(),generation:text('generation').notNull(),ownerToken:text('owner_token').notNull(),expiresAt:integer('expires_at').notNull(),
});

export const firecrawlUsagePeriods = sqliteTable('firecrawl_usage_periods', {
 keyHash:text('key_hash').notNull(),periodStart:text('period_start').notNull(),periodEnd:text('period_end').notNull(),providerUsedHigh:integer('provider_used_high').notNull().default(0),remainingCredits:integer('remaining_credits').notNull(),planCredits:integer('plan_credits').notNull(),checkedAt:integer('checked_at').notNull(),
},t=>[uniqueIndex('firecrawl_period_identity').on(t.keyHash,t.periodStart,t.periodEnd)]);
export const firecrawlUsageEvents = sqliteTable('firecrawl_usage_events', {
 id:text('id').primaryKey(),keyHash:text('key_hash').notNull(),profile:text('profile').notNull(),periodStart:text('period_start').notNull(),periodEnd:text('period_end').notNull(),operation:text('operation').notNull(),reservedCredits:integer('reserved_credits').notNull(),chargedCredits:real('charged_credits'),outcome:text('outcome').notNull(),startedAt:integer('started_at').notNull(),finishedAt:integer('finished_at'),activeUntil:integer('active_until').notNull(),providerStatus:integer('provider_status'),errorCode:text('error_code'),generation:text('generation').notNull(),
},t=>[index('firecrawl_budget_period_idx').on(t.periodStart,t.periodEnd,t.profile,t.keyHash),index('firecrawl_rate_idx').on(t.startedAt)]);

export const vacancyClassificationJobs=sqliteTable('vacancy_classification_jobs', {id:text('id').primaryKey(),generation:text('generation').notNull(),requestKey:text('request_key').notNull(),state:text('state').notNull().default('queued'),itemsJson:text('items_json').notNull(),ownerToken:text('owner_token').notNull().default(''),leaseUntil:integer('lease_until').notNull().default(0),createdAt:integer('created_at').notNull(),updatedAt:integer('updated_at').notNull(),durationMs:integer('duration_ms').notNull().default(0),reason:text('reason').notNull().default('')},t=>[uniqueIndex('classification_job_request_identity').on(t.generation,t.requestKey),index('classification_job_generation_idx').on(t.generation,t.createdAt)]);
export const vacancyClassificationSlots=sqliteTable('vacancy_classification_slots',{slot:integer('slot').primaryKey(),generation:text('generation').notNull(),ownerToken:text('owner_token').notNull(),expiresAt:integer('expires_at').notNull()});

export const vacancyDetailSettings=sqliteTable('vacancy_detail_settings', {id:integer('id').primaryKey(),markdown:text('markdown').notNull(),promptRevision:integer('prompt_revision').notNull().default(0),provider:text('provider').notNull().default('openrouter'),openaiModel:text('openai_model').notNull().default(''),reasoningEffort:text('reasoning_effort').notNull().default('medium'),modelRevision:integer('model_revision').notNull().default(0),updatedAt:text('updated_at').notNull().default(sql`CURRENT_TIMESTAMP`)});
export const vacancyRawSnapshots=sqliteTable('vacancy_raw_snapshots', {id:text('id').primaryKey(),candidateId:integer('candidate_id').notNull().references(()=>vacancyCandidates.id,{onDelete:'cascade'}),sourceUrl:text('source_url').notNull(),finalUrl:text('final_url').notNull(),markdownRaw:text('markdown_raw'),contentHash:text('content_hash').notNull(),fetchedAt:text('fetched_at').notNull(),firecrawlMode:text('firecrawl_mode').notNull(),firecrawlCredits:real('firecrawl_credits'),contentStatus:text('content_status').notNull(),diagnostics:text('diagnostics').notNull(),createdAt:text('created_at').notNull()});
export const vacancyRawSnapshotParts=sqliteTable('vacancy_raw_snapshot_parts', {snapshotId:text('snapshot_id').notNull().references(()=>vacancyRawSnapshots.id,{onDelete:'cascade'}),partIndex:integer('part_index').notNull(),markdownRaw:text('markdown_raw').notNull()},t=>[primaryKey({columns:[t.snapshotId,t.partIndex]})]);
export const vacancyContentFetches=sqliteTable('vacancy_content_fetches', {id:text('id').primaryKey(),candidateId:integer('candidate_id').notNull().references(()=>vacancyCandidates.id,{onDelete:'cascade'}),snapshotId:text('snapshot_id').references(()=>vacancyRawSnapshots.id,{onDelete:'cascade'}),sourceUrl:text('source_url').notNull(),finalUrl:text('final_url'),firecrawlMode:text('firecrawl_mode').notNull(),credits:real('credits'),startedAt:integer('started_at').notNull(),durationMs:integer('duration_ms').notNull(),status:text('status').notNull(),errorCode:text('error_code'),message:text('message').notNull().default('')});
export const vacancyExtractions=sqliteTable('vacancy_extractions', {id:text('id').primaryKey(),candidateId:integer('candidate_id').notNull().references(()=>vacancyCandidates.id,{onDelete:'cascade'}),snapshotId:text('snapshot_id').notNull().references(()=>vacancyRawSnapshots.id,{onDelete:'cascade'}),provider:text('provider').notNull(),modelId:text('model_id'),configHash:text('config_hash').notNull(),promptVersion:text('prompt_version').notNull(),schemaVersion:integer('schema_version').notNull(),extractedData:text('extracted_data'),processedAt:text('processed_at').notNull(),durationMs:integer('duration_ms').notNull(),inputTokens:integer('input_tokens'),outputTokens:integer('output_tokens'),costUsd:real('cost_usd'),status:text('status').notNull(),errorCode:text('error_code'),diagnostics:text('diagnostics').notNull().default('{}')});
export const vacancyDetailJobs=sqliteTable('vacancy_detail_jobs', {id:text('id').primaryKey(),generation:text('generation').notNull(),requestKey:text('request_key').notNull(),itemsJson:text('items_json').notNull(),state:text('state').notNull().default('queued'),ownerToken:text('owner_token').notNull().default(''),leaseUntil:integer('lease_until').notNull().default(0),createdAt:integer('created_at').notNull(),updatedAt:integer('updated_at').notNull(),durationMs:integer('duration_ms').notNull().default(0),reason:text('reason').notNull().default('')});
export const vacancyDetailLeases=sqliteTable('vacancy_detail_leases', {leaseKey:text('lease_key').primaryKey(),generation:text('generation').notNull(),ownerToken:text('owner_token').notNull(),expiresAt:integer('expires_at').notNull()});

// Safe parser import sidecars (SQL0059).
import {foreignKey} from 'drizzle-orm/sqlite-core';
export const vacancyImportOperations=sqliteTable('vacancy_import_operations', {id:text('id').primaryKey(),candidateId:integer('candidate_id').references(()=>vacancyCandidates.id,{onDelete:'set null'}),vacancyId:integer('vacancy_id').references(()=>companyVacancies.id,{onDelete:'set null'}),batchId:text('batch_id'),action:text('action').notNull(),status:text('status').notNull(),processedAt:text('processed_at').notNull(),inputHash:text('input_hash'),changesJson:text('changes_json').notNull(),errorCode:text('error_code'),message:text('message').notNull().default('')});
export const vacancyParserFields=sqliteTable('vacancy_parser_fields', {vacancyId:integer('vacancy_id').notNull().references(()=>companyVacancies.id,{onDelete:'cascade'}),field:text('field').notNull(),valueJson:text('value_json').notNull(),manual:integer('manual').notNull().default(0),candidateId:integer('candidate_id')},t=>[primaryKey({columns:[t.vacancyId,t.field]})]);
export const vacancyParserRoles=sqliteTable('vacancy_parser_roles', {vacancyId:integer('vacancy_id').notNull(),roleId:integer('role_id').notNull(),candidateId:integer('candidate_id').notNull().references(()=>vacancyCandidates.id,{onDelete:'cascade'})},t=>[primaryKey({columns:[t.vacancyId,t.roleId,t.candidateId]}),foreignKey({columns:[t.vacancyId,t.roleId],foreignColumns:[vacancyRoleAssignments.vacancyId,vacancyRoleAssignments.roleId]}).onDelete('cascade')]);
export const vacancyPublicationKeys=sqliteTable('vacancy_publication_keys', {identityKey:text('identity_key').primaryKey(),vacancyId:integer('vacancy_id').notNull().references(()=>companyVacancies.id,{onDelete:'cascade'})});
export const vacancyPublicationSources=sqliteTable('vacancy_publication_sources', {candidateId:integer('candidate_id').primaryKey().references(()=>vacancyCandidates.id,{onDelete:'cascade'}),vacancyId:integer('vacancy_id').notNull().references(()=>companyVacancies.id,{onDelete:'cascade'}),url:text('url').notNull(),canonicalUrl:text('canonical_url').notNull(),sourceNamespace:text('source_namespace'),externalJobId:text('external_job_id'),extractionId:text('extraction_id').references(()=>vacancyExtractions.id,{onDelete:'set null'}),updatedAt:text('updated_at').notNull()});
export const vacancyRoleResults=sqliteTable('vacancy_role_results', {id:text('id').primaryKey(),candidateId:integer('candidate_id').notNull().references(()=>vacancyCandidates.id,{onDelete:'cascade'}),snapshotId:text('snapshot_id').notNull().references(()=>vacancyRawSnapshots.id,{onDelete:'cascade'}),inputHash:text('input_hash').notNull(),treeHash:text('tree_hash').notNull(),promptVersion:text('prompt_version').notNull(),configHash:text('config_hash').notNull(),modelId:text('model_id'),provider:text('provider').notNull(),roleIds:text('role_ids'),status:text('status').notNull(),errorCode:text('error_code'),message:text('message').notNull().default(''),processedAt:text('processed_at').notNull(),durationMs:integer('duration_ms').notNull(),inputTokens:integer('input_tokens'),outputTokens:integer('output_tokens'),costUsd:real('cost_usd')});
export const vacancyRoleSettings=sqliteTable('vacancy_role_settings', {id:integer('id').primaryKey(),markdown:text('markdown').notNull(),promptRevision:integer('prompt_revision').notNull().default(0),provider:text('provider').notNull().default('openrouter'),openaiModel:text('openai_model').notNull().default(''),openrouterModel:text('openrouter_model').notNull().default('best_free'),reasoningEffort:text('reasoning_effort').notNull().default('medium'),modelRevision:integer('model_revision').notNull().default(0),updatedAt:text('updated_at').notNull().default(sql`CURRENT_TIMESTAMP`)});
export const vacancyImportWriteContext=sqliteTable('vacancy_import_write_context',{vacancyId:integer('vacancy_id').primaryKey(),generation:text('generation').notNull(),ownerToken:text('owner_token').notNull()});
export const vacancyImportBatches=sqliteTable('vacancy_import_batches',{id:text('id').primaryKey(),generation:text('generation').notNull(),requestKey:text('request_key').notNull(),itemsJson:text('items_json').notNull(),state:text('state').notNull().default('pending'),createdAt:text('created_at').notNull()},t=>[uniqueIndex('import_batch_request_identity').on(t.generation,t.requestKey)]);

// Durable operational scheduler state is generation fenced and excluded from backups.
export const schedulerJobs=sqliteTable('scheduler_jobs',{id:text('id').primaryKey(),generation:text('generation').notNull(),dedupeKey:text('dedupe_key').notNull(),type:text('type').notNull(),companyId:integer('company_id'),agencyId:integer('agency_id'),candidateId:integer('candidate_id'),sourceId:text('source_id'),payload:text('payload').notNull().default('{}'),status:text('status').notNull().default('pending'),priority:integer('priority').notNull(),runAfter:integer('run_after').notNull(),attempts:integer('attempts').notNull().default(0),maxAttempts:integer('max_attempts').notNull().default(4),lockedBy:text('locked_by').notNull().default(''),lockedUntil:integer('locked_until').notNull().default(0),lastError:text('last_error').notNull().default(''),reasonCode:text('reason_code').notNull().default(''),createdAt:integer('created_at').notNull(),updatedAt:integer('updated_at').notNull(),completedAt:integer('completed_at')});
export const schedulerRuns=sqliteTable('scheduler_runs',{id:text('id').primaryKey(),generation:text('generation').notNull(),trigger:text('trigger').notNull(),startedAt:integer('started_at').notNull(),finishedAt:integer('finished_at'),status:text('status').notNull(),ready:integer('ready').notNull().default(0),processed:integer('processed').notNull().default(0),succeeded:integer('succeeded').notNull().default(0),errors:integer('errors').notNull().default(0),remaining:integer('remaining').notNull().default(0),reason:text('reason').notNull().default(''),cursorId:integer('cursor_id').notNull().default(0),leaseUntil:integer('lease_until').notNull().default(0)});
export const schedulerAttempts=sqliteTable('scheduler_attempts',{id:text('id').primaryKey(),jobId:text('job_id').notNull(),runId:text('run_id').notNull(),type:text('type').notNull(),companyId:integer('company_id'),sourceId:text('source_id'),startedAt:integer('started_at').notNull(),finishedAt:integer('finished_at'),status:text('status').notNull(),reasonCode:text('reason_code').notNull().default(''),message:text('message').notNull().default(''),provider:text('provider'),model:text('model'),reservedUsd:real('reserved_usd').notNull().default(0),costUsd:real('cost_usd'),credits:real('credits'),resultJson:text('result_json').notNull().default('{}')});

// Apify account connection; run/budget ledger is operational and not restored.
export const apifySettings = sqliteTable('apify_settings', {
 id:integer('id').primaryKey(), enabled:integer('enabled').notNull().default(0), paidAllowed:integer('paid_allowed').notNull().default(0), revision:integer('revision').notNull().default(0),
 freeKeyEncrypted:text('free_key_encrypted').notNull().default(''),freeKeySuffix:text('free_key_suffix').notNull().default(''),starterKeyEncrypted:text('starter_key_encrypted').notNull().default(''),starterKeySuffix:text('starter_key_suffix').notNull().default(''),
 freeAccountJson:text('free_account_json').notNull().default(''),starterAccountJson:text('starter_account_json').notNull().default(''),freeError:text('free_error').notNull().default(''),starterError:text('starter_error').notNull().default('')
});
export const apifyRuns = sqliteTable('apify_runs', {
 id:text('id').primaryKey(),generation:text('generation').notNull(),operationKey:text('operation_key').notNull(),profile:text('profile').notNull(),accountId:text('account_id').notNull(),keyHash:text('key_hash').notNull(),periodStart:text('period_start').notNull(),periodEnd:text('period_end').notNull(),reservedUsd:real('reserved_usd').notNull(),chargedUsd:real('charged_usd'),state:text('state').notNull(),runId:text('run_id').notNull().default(''),datasetId:text('dataset_id').notNull().default(''),errorCode:text('error_code').notNull().default(''),createdAt:integer('created_at').notNull(),updatedAt:integer('updated_at').notNull()
},t=>[uniqueIndex('apify_operation_idx').on(t.generation,t.operationKey),index('apify_budget_idx').on(t.accountId,t.periodStart,t.periodEnd)]);

// Account-wide usage survives restore independently of settings/keys/profile.
export const apifyAccountUsage=sqliteTable('apify_account_usage',{id:text('id').primaryKey(),accountId:text('account_id').notNull(),periodStart:text('period_start').notNull(),periodEnd:text('period_end').notNull(),providerUsedUsd:real('provider_used_usd').notNull(),updatedAt:integer('updated_at').notNull()});

// Operational daily parser-error peaks; excluded from user backup/restore.
export const parserErrorDaily=sqliteTable('parser_error_daily',{ownerName:text('owner_name').notNull(),day:text('day').notNull(),p1Peak:integer('p1_peak').notNull(),p2Peak:integer('p2_peak').notNull(),firstSampleAt:integer('first_sample_at').notNull(),lastSampleAt:integer('last_sample_at').notNull()},table=>[primaryKey({columns:[table.ownerName,table.day]})]);

// Immutable operational acquisition provenance survives ordinary retries/restore locally.
export const parserCandidateIntakes=sqliteTable('parser_candidate_intakes',{candidateToken:text('candidate_token').primaryKey().notNull(),parser:text('parser').notNull(),sourceId:text('source_id'),receivedAt:text('received_at').notNull()});
