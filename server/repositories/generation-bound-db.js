import {runtimeGenerationSql, restoredDuringOperation} from './runtime-generation.js';
const contexts = new WeakMap();
export const boundGeneration = db => contexts.get(db);

// Request-local D1 facade: guard and every statement execute in ONE transaction.
// Covers bootstrap, reads and writes without rewriting arbitrary application SQL.
export function generationBoundDb(db, generation, operationGuard) {
 if (typeof generation !== 'string' || !generation) throw new Error('Не задано поколение операции.');
 const originals = new WeakMap();
 async function batch(statements) {
  try {
   const results = await db.batch([
    db.prepare(`SELECT CASE WHEN ${runtimeGenerationSql} = ? THEN 1
      ELSE abs(-9223372036854775808) END AS generation_guard`).bind(generation),
    ...(operationGuard ? [db.prepare(`SELECT CASE WHEN (${operationGuard.sql}) THEN 1
      ELSE abs(-9223372036854775808) END AS operation_guard`).bind(...operationGuard.bindings)] : []),
    ...statements,
   ]);
   return results.slice(operationGuard ? 2 : 1);
  } catch (error) {
   // Read only after rollback. Do not classify a genuine later overflow as restore.
   const latest = await db.prepare('SELECT generation FROM app_runtime_generation WHERE id = 1').first();
   if (latest?.generation !== generation) throw restoredDuringOperation();
   if (operationGuard) {
    const active = await db.prepare(`SELECT (${operationGuard.sql}) AS active`).bind(...operationGuard.bindings).first();
    if (!active?.active) throw operationGuard.failure();
   }
   throw error;
  }
 }
 function wrap(original) {
  const statement = {
   bind(...args) {return wrap(original.bind(...args));},
   async first(column) {
    const row = (await batch([original]))[0].results?.[0] ?? null;
    return column && row ? row[column] : row;
   },
   async all() {return (await batch([original]))[0];},
   async run() {return (await batch([original]))[0];},
  };
  originals.set(statement, original);
  return statement;
 }
 const bound = {
  prepare(sql) {return wrap(db.prepare(sql));},
  async batch(statements) {
   return batch(statements.map(statement => {
    const original = originals.get(statement);
    if (!original) throw new Error('Statement принадлежит другому подключению.');
    return original;
   }));
  },
 };
 contexts.set(bound, generation);
 return bound;
}
