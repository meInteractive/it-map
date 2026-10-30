import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const [{ default: CompanyList }, { default: Vacancies },
  { defaultFilters }, { defaultVacancyFilters }] = await Promise.all([
  moduleFor('frontend/features/companies/CompanyList.tsx'),
  moduleFor('frontend/features/vacancies/Vacancies.tsx'),
  moduleFor('frontend/features/companies/Filters.tsx'),
  moduleFor('frontend/features/vacancies/model.ts'),
]);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const previous = globalThis.fetch;
const calls = [];
let root;
const nav = { openCompany() {}, openChannel() {}, openVacancy() {} };
const counts = [];
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
const find = name => root.root.findAll(n => n.props.className === name);
try {
  // Только синтетический API, без сетевых запросов и MariaDB.
  globalThis.fetch = async url => {
    calls.push(url);
    if (url === '/api/companies/list') return Response.json({ companies: [] });
    if (url === '/api/channels') return Response.json({ channels: [] });
    if (url.startsWith('/api/vacancies/list?period=')) {
      return Response.json({
        entities: [{ name: 'Тестовый работодатель', rank: 1, entityType: 'company' }],
        vacancies: [{
          id: 1, title: 'Тестовая вакансия', companyName: 'Тестовый работодатель',
          entityType: 'company', publishedAt: '2026-10-25', roles: [],
          hidden: false, favorite: false, level: 'Senior',
        }],
        applicationSummary: [], roleTree: [],
      });
    }
    throw Error('Неожиданный тестовый запрос: ' + url);
  };
  const listProps = {
    nav, filters: defaultFilters, query: '', onCount: n => counts.push(n), admin: false,
  };
  // 18. Пустой каталог компаний после завершения загрузки.
  await act(async () => { root = create(h(CompanyList, listProps)); });
  await flush();
  assert.equal(find('company-list-empty').length, 1);
  assert.match(find('company-list-empty')[0].children.join(''), /В выбранных диапазонах записей нет/);
  assert.equal(find('company-list-table employer-list-table').length, 0);
  assert.equal(counts.at(-1), 0);

  // 20. Пустой раздел каналов остаётся доступен и не выглядит как ошибка API.
  await act(async () => root.update(h(CompanyList, { ...listProps, type: 'channel' })));
  await flush();
  assert.equal(find('company-list-empty').length, 1);
  assert.equal(find('company-list-table channel-list-table').length, 0);
  assert.equal(counts.at(-1), 0);
  await act(async () => root.unmount()); root = undefined;

  // 19. API вернул одну вакансию, но фильтр поисковой строки исключает её.
  await act(async () => {
    root = create(h(Vacancies, {
      nav, admin: false, filters: defaultFilters, vacancyFilters: defaultVacancyFilters,
      query: 'несуществующая вакансия', onCount: n => counts.push(n), type: 'company',
    }));
  });
  await flush();
  assert.equal(find('vacancies-empty').length, 1);
  assert.match(find('vacancies-empty')[0].children.join(''), /В выбранных фильтрах вакансий нет/);
  assert.equal(root.root.findAll(n => n.props.className === 'empty' && n.children.includes('Загрузка…')).length, 0);
  assert.equal(counts.at(-1), 0);
  assert.ok(calls.includes('/api/companies/list'));
  assert.ok(calls.includes('/api/channels'));
  assert.ok(calls.some(url => url.startsWith('/api/vacancies/list?period=')));
  console.log('PASS: пустые списки компаний/каналов и отсутствие вакансий после фильтрации');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = previous;
}
