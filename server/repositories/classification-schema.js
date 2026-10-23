// Role-bound prompts extend the prompt repository. The legacy system_prompts
// kind CHECK remains unchanged so cover-letter/relevance contracts stay intact.
export const CLASSIFICATION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS vacancy_role_classification (
 role_id INTEGER PRIMARY KEY NOT NULL REFERENCES vacancy_roles(id) ON DELETE CASCADE,
 markdown TEXT NOT NULL DEFAULT '' CHECK(length(markdown)<=10000),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
 parsing_enabled INTEGER NOT NULL DEFAULT 0 CHECK(parsing_enabled IN (0,1)),
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;
