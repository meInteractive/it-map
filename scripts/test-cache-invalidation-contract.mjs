import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

// Независимый эталон исходного поведения до GO-115: проверяем реальные кэш-запросы,
// а не результат вызова той же функции-предиката, которую изменяем.
function originalInvalidation(path, key) {
  if (/^\/api\/vacancies\/\d+\/relevance$/.test(path))
    return key === path.replace('/relevance', '');
  if (path.endsWith('/cover-letter'))
    return key === path.replace('/cover-letter', '');

  const catalog = /^\/api\/(companies|vacancies|applications|crm|backup)/.test(path);
  return (
    key === path ||
    ((catalog || path.startsWith('/api/firecrawl/') || path.startsWith('/api/scheduler')) &&
      (key === '/api/apify/hh-sources' || key.startsWith('/api/apify/hh-source?'))) ||
    (catalog && /^\/api\/(companies|agencies|vacancies|applications|crm|tasks|channels)/.test(key)) ||
    (/^\/api\/vacancy-roles/.test(path) &&
      /^\/api\/(vacancy-roles|vacancies|classification)/.test(key)) ||
    (path.startsWith('/api/apify/') &&
      (key.startsWith('/api/apify/') ||
        key.startsWith('/api/firecrawl/') ||
        key.startsWith('/api/scheduler'))) ||
    (path.startsWith('/api/firecrawl/') &&
      (key.startsWith('/api/firecrawl/') || key.startsWith('/api/scheduler'))) ||
    (path.startsWith('/api/scheduler') &&
      (key.startsWith('/api/scheduler') || key.startsWith('/api/firecrawl/'))) ||
    (catalog && key.startsWith('/api/scheduler')) ||
    (catalog && /^\/api\/firecrawl\/(sources|pending)(?:[?]|$)/.test(key)) ||
    (path.startsWith('/api/classification/') &&
      (key === '/api/classification/settings' ||
        key === '/api/classification/jobs' ||
        key.startsWith('/api/scheduler'))) ||
    (/^\/api\/vacancy-(details|import)/.test(path) && key.startsWith('/api/scheduler')) ||
    ((path.startsWith('/api/firecrawl/') || path.startsWith('/api/vacancy-candidates')) &&
      key === '/api/classification/jobs') ||
    (/^\/api\/tasks/.test(path) && key.startsWith('/api/tasks')) ||
    (/^\/api\/google-calendar/.test(path) && /^\/api\/(google-calendar|tasks)/.test(key)) ||
    (/^\/api\/channels/.test(path) && /^\/api\/(channels|vacancies)/.test(key)) ||
    (/^\/api\/hiring-contacts/.test(path) && key.startsWith('/api/hiring-contacts')) ||
    (/^\/api\/openrouter/.test(path) && key.startsWith('/api/openrouter')) ||
    (/^\/api\/openai/.test(path) && key.startsWith('/api/openai')) ||
    (/^\/api\/profile/.test(path) && key.startsWith('/api/profile'))
  );
}

const mutations = [
  '/api/companies', '/api/companies/2', '/api/companiesX',
  '/api/agencies', '/api/vacancies', '/api/vacancies/71',
  '/api/vacancies/71/relevance', '/api/vacancies/71/cover-letter',
  '/api/vacancies/71/cover-letter/regenerate', '/api/applications/7',
  '/api/crm/applications', '/api/backup', '/api/vacancy-roles/8',
  '/api/vacancy-details/7', '/api/vacancy-import/7',
  '/api/vacancy-candidates/7', '/api/apify/runs',
  '/api/firecrawl/sources', '/api/scheduler/jobs', '/api/scheduler',
  '/api/classification/settings', '/api/classification/prompts/7',
  '/api/tasks/5', '/api/google-calendar/events', '/api/channels/1',
  '/api/hiring-contacts/5', '/api/openrouter/settings',
  '/api/openai/settings', '/api/profile', '/api/session',
  '/api/unrelated', '/api/firecrawlx', '/api/schedulerx'
];
const keys = [...new Set([
  ...mutations,
  '/api/companies?limit=10', '/api/agencies?limit=10',
  '/api/vacancies/71', '/api/vacancies?role=1',
  '/api/applications?company=7', '/api/crm/applications?status=draft',
  '/api/apify/hh-sources', '/api/apify/hh-source?source=7',
  '/api/apify/hh-source', '/api/apify/runs?status=recent',
  '/api/firecrawl/sources', '/api/firecrawl/sources?x=1',
  '/api/firecrawl/pending', '/api/firecrawl/pending?x=1',
  '/api/firecrawl/sources?x=1/other', '/api/firecrawl/sources-extra',
  '/api/firecrawl/history', '/api/scheduler/funnel?days=7',
  '/api/classification/jobs', '/api/classification/settings',
  '/api/classification/settings?x=1', '/api/vacancy-roles',
  '/api/tasks?limit=5', '/api/google-calendar/events',
  '/api/channels?limit=5', '/api/hiring-contacts?sort=name',
  '/api/openrouter/models', '/api/openai/models', '/api/profile/candidate',
  '/api/roles', '/api/public'
])];

assert.equal(originalInvalidation('/api/vacancies/71/relevance', '/api/vacancies/71'), true);
assert.equal(originalInvalidation('/api/vacancies/71/relevance', '/api/vacancies'), false);
assert.equal(originalInvalidation('/api/vacancies/71/cover-letter', '/api/vacancies/71'), true);
assert.equal(originalInvalidation('/api/apify/runs', '/api/scheduler/jobs'), true);
assert.equal(originalInvalidation('/api/companies/2', '/api/firecrawl/history'), false);
assert.equal(originalInvalidation('/api/firecrawl/sources', '/api/classification/jobs'), true);

const api = await moduleFor('frontend/api/client.ts');
const oldFetch = globalThis.fetch;
let checked = 0;
try {
  for (const path of mutations) {
    for (const key of keys) {
      api.clearCache();
      let fetches = 0;
      globalThis.fetch = async () => {
        fetches++;
        return Response.json({ synthetic: true });
      };
      await api.load(key);
      assert.equal(fetches, 1, 'Начальная загрузка ' + key);

      const events = [];
      const unsubscribe = api.onCacheEvent((event) => events.push(event));
      try {
        api.invalidateMutation(path, path === '/api/applications/7' ? 'DELETE' : 'PUT');
      } finally {
        unsubscribe();
      }
      assert.deepEqual(events, [{ type: 'mutation', path }], 'Событие мутации ' + path);

      await api.load(key);
      const expected = originalInvalidation(path, key);
      assert.equal(
        fetches,
        expected ? 2 : 1,
        'Неверная инвалидaция: mutation=' + path + ', cached=' + key
      );
      checked++;
    }
  }
  console.log('PASS GO-115: ' + checked + ' сочетаний mutations/cache keys сохраняют исходный контракт');
} finally {
  api.clearCache();
  globalThis.fetch = oldFetch;
}
