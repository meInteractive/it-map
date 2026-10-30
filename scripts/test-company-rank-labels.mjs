import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { companyRankLabel } = await moduleFor('frontend/features/companies/labels.ts');
const { mapCompanyLabel } = await moduleFor('frontend/features/map/labels.ts');

for (const [rank, prefix] of [
  [1, '№ 1'], [50, '№ 50'], ['101', '№ 101'],
  [null, ''], [undefined, ''], [0, ''], [-1, ''],
  [NaN, ''], [Infinity, ''], [1.5, ''], ['не задан', ''],
]) {
  assert.equal(companyRankLabel(rank), prefix, `Подпись списка: ${String(rank)}`);
  assert.equal(mapCompanyLabel({ name: 'Синтетическая компания', rank }),
    [prefix, 'Синтетическая компания'].filter(Boolean).join(' '),
    `Подпись карты: ${String(rank)}`);
}
// Список возвращает только номер, карта добавляет название: контракты разные.
assert.equal(companyRankLabel(2), '№ 2');
assert.equal(mapCompanyLabel({ name: 'Компания А', rank: 2 }), '№ 2 Компания А');
console.log('PASS: положительный рейтинг и отсутствие ложных номеров в списке и на карте');
