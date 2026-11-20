import { test, expect, type Page, type Route } from '@playwright/test';
import { build } from 'esbuild';

// Интеграция настоящего React-клиента с контролируемым синтетическим HTTP.
// Серверный период и SQL подтверждаются отдельно существующими Go/interop тестами.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "import { visibleVacancy } from './frontend/features/vacancies/model';",
      "window.__ciVacancyVisible = visibleVacancy;",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'vacancy-filters-fixture.tsx',
    resolveDir: process.cwd(),
    loader: 'tsx',
  },
  bundle: true,
  write: false,
  platform: 'browser',
  format: 'iife',
  jsx: 'automatic',
  plugins: [{
    name: 'empty-test-css',
    setup(compiler) {
      compiler.onResolve({ filter: /\.css$/ }, (args) => ({
        path: args.path, namespace: 'test-css',
      }));
      compiler.onLoad({ filter: /.*/, namespace: 'test-css' }, () => ({
        contents: '', loader: 'js',
      }));
    },
  }],
});
const script = bundle.outputFiles[0].text;
const engineering = {
  id: 10, stableKey: 'eng', name: 'Инженерия', parentId: null, sortOrder: 1,
  usageCount: 1, childCount: 1, isLeaf: false, path: 'IT → Инженерия',
  descendantIds: [11],
};
const backend = {
  id: 11, stableKey: 'backend', name: 'Бэкенд', parentId: 10, sortOrder: 1,
  usageCount: 1, childCount: 0, isLeaf: true, path: 'IT → Инженерия → Бэкенд',
  descendantIds: [],
};
const design = {
  id: 20, stableKey: 'design', name: 'Дизайн', parentId: null, sortOrder: 2,
  usageCount: 1, childCount: 0, isLeaf: true, path: 'IT → Дизайн',
  descendantIds: [],
};
const roleTree = [engineering, backend, design];
const roleCatalog = {
  roles: roleTree,
  tree: [{ ...engineering, children: [backend] }, design],
  roots: [engineering, design],
  leaves: [backend, design],
};
const company = {
  id: 1, name: 'CI работодатель', entityType: 'company',
  rank: 1, favorite: false, hidden: false, offices: [],
};
const agency = {
  id: 2, name: 'CI агентство', entityType: 'agency',
  rank: 2, favorite: false, hidden: false, offices: [],
};
function vacancy(id: number, title: string, owner = company, role = backend) {
  return {
    id, title, companyName: owner.name, entityType: owner.entityType,
    realCompanyName: owner === agency ? 'CI заказчик' : '',
    salaryFrom: null, salaryTo: null, role: role.name, roles: [role],
    level: 'Lead', relevance: null, favorite: false, hidden: false,
    archived: false, publishedAt: '2026-11-19', loadedAt: null,
    createdAt: '2026-11-19 10:00:00', applicationId: null,
    applicationStatus: '', telegramSources: [],
  };
}
const list = (...records: ReturnType<typeof vacancy>[]) => ({
  vacancies: records, roleTree, entities: [company, agency], applicationSummary: [],
});
type Answer = { body: unknown; status?: number };
type Resolver = (period: string) => Promise<Answer> | Answer;
async function respond(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status, contentType: 'application/json', body: JSON.stringify(body),
  });
}
async function fixture(page: Page, resolver: Resolver) {
  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.resourceType() === 'document') {
      await route.fulfill({
        status: 200, contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
      return;
    }
    if (path === '/api/session') return respond(route, { name: 'CI Demo', isAdmin: false });
    if (path === '/api/vacancy-roles') return respond(route, roleCatalog);
    if (path === '/api/vacancies/list') {
      const answer = await resolver(url.searchParams.get('period') || '');
      return respond(route, answer.body, answer.status);
    }
    if (path.startsWith('/api/'))
      return respond(route, { error: 'Unexpected synthetic request: ' + path }, 500);
    await route.abort('blockedbyclient');
  });
  await page.goto('/vacancies');
  await page.addScriptTag({ content: script });
  await expect(page.getByRole('combobox', { name: 'Дата публикации вакансии' })).toBeVisible();
}

test('S11-B04-04/05: категория включает конечного потомка, снятие роли сохраняет поиск', async ({ page }) => {
  await fixture(page, () => ({
    body: list(vacancy(101, 'CI backend engineer'), vacancy(102, 'CI product designer', company, design)),
  }));
  await expect(page.getByRole('button', { name: 'CI backend engineer' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI product designer' })).toBeVisible();
  await page.locator('.vacancy-search input').fill('CI');
  await page.getByRole('button', { name: /Роль · Все/ }).click();
  await page.getByRole('checkbox', { name: /Инженерия/ }).check();
  await expect(page.getByRole('button', { name: 'CI backend engineer' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI product designer' })).toHaveCount(0);
  await page.getByRole('checkbox', { name: /Инженерия/ }).uncheck();
  await expect(page.getByRole('button', { name: 'CI product designer' })).toBeVisible();
  await expect(page.locator('.vacancy-search input')).toHaveValue('CI');
});

test('S11-B04-06: период пересекается с типом источника, а не сбрасывает его', async ({ page }) => {
  const companyRow = vacancy(201, 'CI вакансия компании');
  const agencyRow = vacancy(202, 'CI вакансия агентства', agency);
  const periods: string[] = [];
  await fixture(page, (period) => {
    periods.push(period);
    return { body: period === 'week' ? list(companyRow) : list(companyRow, agencyRow) };
  });
  await page.getByRole('combobox', { name: 'Дата публикации вакансии' }).selectOption('month');
  await expect(page.getByRole('button', { name: 'CI вакансия компании' })).toBeVisible();
  const nav = page.locator('[data-section-menu="vacancies"]');
  await nav.locator('.settings-nav-trigger').click();
  await nav.getByRole('button', { name: 'Агентства' }).click();
  await expect(page.getByRole('heading', { name: 'Вакансии агентств' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI вакансия агентства' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI вакансия компании' })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Дата публикации вакансии' })).toHaveValue('month');
  expect(periods).toContain('month');
});

test('S11-B04-07: неизвестная роль даёт безопасно пустую выборку', async ({ page }) => {
  await fixture(page, () => ({ body: list(vacancy(301, 'CI backend engineer')) }));
  await expect(page.getByRole('button', { name: 'CI backend engineer' })).toBeVisible();
  const displayed = await page.evaluate(() => {
    const node = (window as unknown as {
      __ciVacancyVisible: (...args: unknown[]) => boolean;
    }).__ciVacancyVisible;
    const entity = {
      name: 'CI работодатель', entityType: 'company', rank: 1,
      favorite: false, hidden: false, hasVacancies: true,
    };
    const record = {
      id: 301, title: 'CI backend engineer', companyName: entity.name,
      entityType: 'company', roles: [{ id: 11 }], hidden: false,
    };
    const filters = {
      rankTop: true, rankBottom: true, rank101to150: true, rank151to200: true,
      rank201to250: true, rank251to300: true, rankBeyond300: true,
      rankUnranked: true, favorites: 'all', showHidden: false,
      vacanciesOnly: false, applicationsOnly: false,
    };
    const vacancyFilters = {
      levels: [], roleIds: [987654321], favoriteOnly: false, showHidden: false,
    };
    return node(record, 'company', new Map([[entity.name, entity]]), filters, vacancyFilters, '', []);
  });
  expect(displayed).toBe(false);
});

test('S11-B04-08: поздний ответ прошлого периода не подменяет новую выдачу', async ({ page }) => {
  let releaseWeek: () => void = () => {};
  const weekGate = new Promise<void>((resolve) => { releaseWeek = resolve; });
  await fixture(page, async (period) => {
    if (period === 'week') {
      await weekGate;
      return { body: list(vacancy(401, 'CI устаревшая неделя')) };
    }
    return { body: list(vacancy(402, 'CI актуальный месяц')) };
  });
  await page.getByRole('combobox', { name: 'Дата публикации вакансии' }).selectOption('month');
  await expect(page.getByRole('button', { name: 'CI актуальный месяц' })).toBeVisible();
  const oldResponse = page.waitForResponse((response) =>
    response.url().includes('/api/vacancies/list?period=week') && response.status() === 200);
  releaseWeek();
  await oldResponse;
  await expect(page.getByRole('button', { name: 'CI актуальный месяц' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI устаревшая неделя' })).toHaveCount(0);
});

test('S11-B04-09/10: ошибка сохраняет фильтры, повтор не добавляет дубликат строк', async ({ page }) => {
  const periods: string[] = [];
  let attempts = 0;
  await fixture(page, (period) => {
    attempts++;
    periods.push(period);
    if (attempts === 1) return { status: 503, body: { error: 'CI временная ошибка чтения' } };
    return {
      body: list(vacancy(501, 'CI восстановленная вакансия'), vacancy(502, 'CI другая')),
    };
  });
  await expect(page.getByRole('alert')).toContainText('CI временная ошибка чтения');
  await page.locator('.vacancy-search input').fill('восстановленная');
  await page.getByRole('button', { name: 'Повторить загрузку' }).click();
  await expect(page.getByRole('button', { name: 'CI восстановленная вакансия' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI восстановленная вакансия' })).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'CI другая' })).toHaveCount(0);
  await expect(page.locator('.vacancy-search input')).toHaveValue('восстановленная');
  await expect(page.getByRole('combobox', { name: 'Дата публикации вакансии' })).toHaveValue('week');
  expect(periods).toEqual(['week', 'week']);
});
