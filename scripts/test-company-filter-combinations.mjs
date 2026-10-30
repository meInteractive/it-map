import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { matchesEntity, defaultFilters } = await moduleFor('frontend/features/companies/Filters.tsx');
const company = (overrides = {}) => ({
  name: 'Синтетическая компания', rank: 42, entityType: 'company',
  favorite: false, hidden: false, hasVacancies: false, hasApplications: false,
  offices: [], ...overrides,
});
const visible = (values = {}, settings = {}) =>
  matchesEntity(company(values), { ...defaultFilters, ...settings });

// 1. Избранное и скрытое — независимые личные признаки.
for (const favorite of [false, true]) {
  for (const hidden of [false, true]) {
    assert.equal(visible({ favorite, hidden }), !hidden);
    assert.equal(visible({ favorite, hidden }, { showHidden: true }), true,
      'Показать скрытые не означает фильтровать по избранным');
    assert.equal(visible({ favorite, hidden }, { favoriteOnly: true }), favorite && !hidden);
    assert.equal(visible({ favorite, hidden },
      { favoriteOnly: true, showHidden: true }), favorite);
  }
}

// 2. Вакансии: включённый vacanciesOnly всегда требует hasVacancies.
for (const hasVacancies of [false, true]) {
  for (const favorite of [false, true]) {
    assert.equal(visible({ hasVacancies, favorite }, { vacanciesOnly: true }), hasVacancies);
    assert.equal(visible({ hasVacancies, favorite },
      { vacanciesOnly: true, favoriteOnly: true }), hasVacancies && favorite);
  }
}

// 3. Отклики: независимы от вакансий и учитывают оба активных ограничения.
for (const hasApplications of [false, true]) {
  for (const hasVacancies of [false, true]) {
    assert.equal(visible({ hasApplications, hasVacancies },
      { applicationsOnly: true }), hasApplications);
    assert.equal(visible({ hasApplications, hasVacancies },
      { applicationsOnly: true, vacanciesOnly: true }), hasApplications && hasVacancies);
  }
}
assert.equal(visible({ favorite: true, hidden: true, hasVacancies: true, hasApplications: true },
  { favoriteOnly: true, showHidden: false, vacanciesOnly: true, applicationsOnly: true }), false);
assert.equal(visible({ favorite: true, hidden: true, hasVacancies: true, hasApplications: true },
  { favoriteOnly: true, showHidden: true, vacanciesOnly: true, applicationsOnly: true }), true);

console.log('PASS: флаги избранного/скрытия, вакансии и отклики — независимо и совместно');
