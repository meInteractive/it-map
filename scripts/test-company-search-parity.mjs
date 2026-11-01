import assert from 'node:assert/strict';
import {moduleFor} from './test-support.mjs';

const {searchCompanies, normalizeCompanySearch} =
  await moduleFor('frontend/features/companies/companySearch.ts');
const {officeFeatures} = await moduleFor('frontend/features/map/geometry.ts');
const {matchesEntity, defaultFilters} =
  await moduleFor('frontend/features/companies/Filters.tsx');

// Проверяем тот же массив данных для списка и карты, без API и геосервиса.
const companies = [
  {name: 'Альфа IT', rank: 1, offices: [{lat: 55.75, lon: 37.61}], entityType: 'company'},
  {name: 'Alpha ИТ', rank: 2, offices: [], entityType: 'company'},
  {name: 'Бета', rank: 3, offices: [{lat: 55.76, lon: 37.62}], entityType: 'company'},
  {name: 'Скрытая Альфа', rank: 4, offices: [{lat: 55.77, lon: 37.63}], hidden: true, entityType: 'company'},
];
const visible = companies.filter(c => matchesEntity(c, defaultFilters));
const cases = [
  ['  АльФА   ', ['Альфа IT']],
  ['alPHA', ['Alpha ИТ']],
  ['ИТ', ['Alpha ИТ']],
  ['альфа', ['Альфа IT']],
  ['  ', ['Альфа IT', 'Alpha ИТ', 'Бета']],
  ['', ['Альфа IT', 'Alpha ИТ', 'Бета']],
  ['несуществующая', []],
];
for (const [term, expected] of cases) {
  const records = searchCompanies(visible, term);
  assert.deepEqual(records.map(c => c.name), expected, 'Список: ' + JSON.stringify(term));
  const markers = officeFeatures(records);
  assert.deepEqual([...new Set(markers.features.map(f => f.properties.companyName))],
    expected.filter(name => visible.find(c => c.name === name).offices.length),
    'Карта: ' + JSON.stringify(term));
  assert.equal(records.some(c => c.name === 'Alpha ИТ') && !markers.features.some(f => f.properties.companyName === 'Alpha ИТ'),
    expected.includes('Alpha ИТ'), 'Компания без геоданных остаётся только в списке');
}
assert.equal(normalizeCompanySearch('  ЁлКИ  '), 'ёлки');
assert.deepEqual(searchCompanies(visible, 'a'), [visible[1]]);
assert.equal(searchCompanies(visible, '').length, 3, 'Очистка восстанавливает полный разрешённый набор');
console.log('PASS: одинаковый поиск названий, пробелы, кириллица/латиница, пустое состояние, маркеры и отсутствие офисов');
