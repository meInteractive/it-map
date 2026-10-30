import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { comparePublication, compareVacancies } = await moduleFor('frontend/features/vacancies/model.ts');
const v = (id, dates = {}) => ({
  id, title: 'Синтетическая вакансия ' + id, companyName: 'Компания',
  entityType: 'company', publishedAt: null, loadedAt: null, createdAt: null,
  ...dates,
});
const order = (records, comparator = comparePublication) =>
  [...records].sort(comparator).map(x => x.id);
const dateSort = (a, b) => compareVacancies(a, b, 'date', 'company', new Set());

// 13. Новая опубликованная вакансия выше старой, независимо от ID.
const old = v(80, { publishedAt: '2026-10-01' });
const middle = v(40, { publishedAt: '2026-10-15' });
const fresh = v(1, { publishedAt: '2026-10-30' });
assert.deepEqual(order([middle, old, fresh]), [1, 40, 80]);
assert.deepEqual(order([old, fresh, middle], dateSort), [1, 40, 80]);

// 14. При равной опубликованной дате сортируем по createdAt, затем по ID.
const equal = [
  v(10, { publishedAt: '2026-10-20', createdAt: '2026-10-12T10:00:00Z' }),
  v(21, { publishedAt: '2026-10-20', createdAt: '2026-10-11T10:00:00Z' }),
  v(22, { publishedAt: '2026-10-20', createdAt: '2026-10-11T10:00:00Z' }),
];
assert.deepEqual(order([equal[1], equal[0], equal[2]]), [10, 22, 21]);
assert.deepEqual(order([equal[2], equal[1], equal[0]], dateSort), [10, 22, 21]);

// 15. Отсутствие всех допустимых дат сортируется после любой валидной даты.
const missing = [
  v(6), v(3, { publishedAt: '2026-02-30', loadedAt: 'invalid', createdAt: null }),
];
const fallback = v(17, { publishedAt: null, loadedAt: '2026-10-05' });
assert.deepEqual(order([missing[1], old, missing[0], fallback]), [17, 80, 6, 3]);
assert.deepEqual(order([missing[0], missing[1], fallback], dateSort), [17, 6, 3]);
assert.equal(comparePublication(v(9), v(3)), -6, 'Если дат нет, ID разрешает равенство');
console.log('PASS: сортировка по публикации, createdAt, ID и отсутствие дат');
