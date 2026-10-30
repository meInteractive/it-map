import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { publicationDate } = await moduleFor('shared/publication-period.ts');
const { vacancyDateLabel } = await moduleFor('frontend/features/vacancies/model.ts');

for (const [source, expected] of [
  ['2026-10-30', '30.10.2026'],
  ['2024-02-29', '29.02.2024'],
  ['2025-02-28', '28.02.2025'],
  [null, '—'], [undefined, '—'], ['', '—'],
  ['2025-02-29', '—'], ['2026-02-30', '—'],
  ['2026-04-31', '—'], ['invalid', '—'],
]) {
  assert.equal(vacancyDateLabel(source), expected,
    `Текст даты публикации: ${String(source)}`);
  const date = publicationDate(source);
  assert.equal(date === null, expected === '—',
    `Контракт parser согласован с подписью: ${String(source)}`);
}
assert.equal(publicationDate('2024-02-29')?.day, 29);
assert.equal(publicationDate('2025-02-29'), null);
console.log('PASS: корректная дата, пустое/ошибочное значение и високосный день');
