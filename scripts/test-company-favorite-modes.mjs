import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { matchesEntity, restoreFilters, defaultFilters, favoriteOptions } =
  await moduleFor('frontend/features/companies/Filters.tsx');
const { officeFeatures } = await moduleFor('frontend/features/map/geometry.ts');

const companies = [
  { name: 'Избранный работодатель', entityType: 'company', rank: 15, favorite: true,
    hidden: false, hasVacancies: true, hasApplications: false,
    offices: [{ lat: 55.751, lon: 37.610, address: 'CI' }] },
  { name: 'Новый работодатель', entityType: 'company', rank: 55, favorite: false,
    hidden: false, hasVacancies: false, hasApplications: false,
    offices: [{ lat: 55.752, lon: 37.611, address: 'CI' }] },
  { name: 'Неизбранный без рейтинга', entityType: 'company', rank: null,
    favorite: false, hidden: false, hasVacancies: false, hasApplications: true,
    offices: [{ lat: 55.753, lon: 37.612, address: 'CI' }] },
  { name: 'Скрытый работодатель', entityType: 'company', rank: 140, favorite: false,
    hidden: true, hasVacancies: false, hasApplications: false,
    offices: [{ lat: 55.754, lon: 37.613, address: 'CI' }] },
];

const names = (items) => items.map((item) => item.name);
const selected = (filters) => companies.filter((company) => matchesEntity(company, filters));
assert.deepEqual(favoriteOptions.map((option) => option.label), [
  'Избранные', 'Не избранные', 'Все',
]);
for (const [mode, expected] of [
  ['favorites', ['Избранный работодатель']],
  ['notFavorites', ['Новый работодатель', 'Неизбранный без рейтинга']],
  ['all', ['Избранный работодатель', 'Новый работодатель', 'Неизбранный без рейтинга']],
]) {
  const filtered = selected({ ...defaultFilters, favorites: mode });
  assert.deepEqual(names(filtered), expected, 'Список: режим ' + mode);
  const mapped = officeFeatures(filtered).features.map((feature) => feature.properties.companyName);
  assert.deepEqual(mapped, expected, 'Карта должна видеть те же компании: ' + mode);
}
assert.deepEqual(names(selected({ ...defaultFilters, favorites: 'notFavorites',
  rankBottom: false })), ['Неизбранный без рейтинга'],
  'Фильтр избранного не должен сбрасывать диапазоны рейтинга');
assert.deepEqual(names(selected({ ...defaultFilters, favorites: 'notFavorites',
  showHidden: true })), ['Новый работодатель', 'Неизбранный без рейтинга', 'Скрытый работодатель'],
  'Режим не избранные совместим с показом скрытых компаний');
assert.deepEqual(names(selected({ ...defaultFilters, favorites: 'notFavorites',
  vacanciesOnly: true })), [], 'Независимый фильтр вакансий сохраняется');

assert.equal(restoreFilters({ version: 1, favoriteOnly: true }).favorites, 'favorites');
assert.equal(restoreFilters({ version: 1, favoriteOnly: false }).favorites, 'all');
assert.equal(restoreFilters({ version: 2, favorites: 'notFavorites' }).favorites, 'notFavorites');
assert.equal(restoreFilters({ version: 2, favorites: 'favorites' }).favorites, 'favorites');
assert.equal(restoreFilters({ version: 2, favorites: 'other' }).favorites, 'all');
assert.equal(restoreFilters({ version: 2, favorites: null }).favorites, 'all');
console.log('PASS: избранные / не избранные / все одинаково для списка и карты, миграция и независимость фильтров');
