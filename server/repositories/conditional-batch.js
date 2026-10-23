// A zero-row conditional UPDATE is a SQL success. Without an in-transaction
// assertion, later statements would still write letters/sources/user state.
// D1 only exposes batch results AFTER the entire transaction has committed.
// SQLite has no RAISE() outside triggers. abs(INT64_MIN) deliberately raises
// "integer overflow" on the losing branch; CASE evaluates only its chosen arm.
// No table, migration, persistent lock or timestamp marker is required.
const requiredUpdateSql = `SELECT CASE WHEN changes() = 1 THEN 1
  ELSE abs(-9223372036854775808) END AS required_update_guard`;

export async function batchWithRequiredUpdate(db, update, following = []) {
  // Contract: update is a single-row UPDATE with its expected-state predicate.
  // Keep the assertion directly after it: another write would replace changes().
  // Do not reclassify errors here: integer overflow in a following statement is
  // not necessarily a stale write. The API caller owns conflict/error handling.
  const results = await db.batch([update, db.prepare(requiredUpdateSql), ...following]);
  return [results[0], ...results.slice(2)];
}
