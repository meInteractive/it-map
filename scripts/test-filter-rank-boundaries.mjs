import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { matchesEntity, defaultFilters } = await moduleFor('frontend/features/companies/Filters.tsx');
const company = rank => ({
  name: 'Синтетическая компания', entityType: 'company', rank,
  favorite: false, hidden: false, hasVacancies: false, hasApplications: false,
  offices: [],
});

const groups = [
  ['rankTop', 1, 50],
  ['rankBottom', 51, 100],
  ['rank101to150', 101, 150],
  ['rank151to200', 151, 200],
  ['rank201to250', 201, 250],
  ['rank251to300', 251, 300],
  ['rankBeyond300', 301, 350],
];
for (const [key, low, high] of groups) {
  for (const rank of [low, high]) {
    assert.equal(matchesEntity(company(rank), defaultFilters), true, `Включённая группа ${key}: ${rank}`);
    const disabled = { ...defaultFilters, [key]: false };
    assert.equal(matchesEntity(company(rank), disabled), false, `Отключённая группа ${key}: ${rank}`);
    for (const [neighbor, min, max] of groups) {
      if (neighbor === key) continue;
      assert.equal(matchesEntity(company(min), disabled), true,
        `Отключение ${key} не должно скрывать соседнюю группу ${neighbor}`);
      assert.equal(matchesEntity(company(max), disabled), true,
        `Отключение ${key} не должно скрывать конец группы ${neighbor}`);
    }
  }
}
for (const rank of [null, undefined, 0, -1, 'нет рейтинга', NaN]) {
  assert.equal(matchesEntity(company(rank), defaultFilters), true, `Без рейтинга: ${String(rank)}`);
  assert.equal(matchesEntity(company(rank), { ...defaultFilters, rankUnranked: false }), false,
    `Выключена группа без рейтинга: ${String(rank)}`);
}
for (const [key, left, right] of [
  ['rankTop', 50, 51], ['rankBottom', 100, 101],
  ['rank101to150', 150, 151], ['rank151to200', 200, 201],
  ['rank201to250', 250, 251], ['rank251to300', 300, 301],
]) {
  const disabled = { ...defaultFilters, [key]: false };
  assert.equal(matchesEntity(company(left), disabled), false);
  assert.equal(matchesEntity(company(right), disabled), true);
}
console.log('PASS: границы 50/51..300/301, отсутствие рейтинга и независимость семи групп');
