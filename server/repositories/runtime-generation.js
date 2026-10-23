// Durable across Worker instances and intentionally excluded from user backups.
export const RUNTIME_GENERATION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS app_runtime_generation (
 id INTEGER PRIMARY KEY NOT NULL,
 generation TEXT NOT NULL,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT app_runtime_generation_singleton CHECK(id = 1),
 CONSTRAINT app_runtime_generation_length CHECK(length(generation) = 36)
)`;
export const runtimeGenerationSql = `(SELECT generation FROM app_runtime_generation WHERE id = 1)`;
export async function ensureRuntimeGeneration(db) {
 await db.prepare(RUNTIME_GENERATION_TABLE_SQL).run();
 await db.prepare('INSERT OR IGNORE INTO app_runtime_generation(id,generation) VALUES(1,?)').bind(crypto.randomUUID()).run();
 const row = await db.prepare('SELECT generation FROM app_runtime_generation WHERE id = 1').first();
 if (!row) throw new Error('Не удалось прочитать поколение базы.');
 return row.generation;
}
// Append INSIDE restore's transaction, after verification. Never restore a former
// generation from the file, and never rotate after COMMIT.
export function rotateRuntimeGenerationStatement(db) {
 return db.prepare(`INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)
  ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,updated_at=CURRENT_TIMESTAMP`)
  .bind(crypto.randomUUID());
}
export function restoredDuringOperation() {
 return Object.assign(new Error('База восстановлена во время операции. Старый результат не сохранён. Обновите страницу и повторите запрос.'), {status:409, code:'restored'});
}
