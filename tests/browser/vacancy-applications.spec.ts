import { test, expect, type Page, type Route } from '@playwright/test';
import { build } from 'esbuild';
import type { ApplicationRecord, VacancyDetail } from '../../shared/contracts';

// Настоящий React, изолированные HTTP-ответы и отдельные личные данные пользователей.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'vacancy-applications-fixture.tsx',
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

const company = {
  id: 1, name: 'CI работодатель C05', entityType: 'company', rank: 1,
  favorite: false, hidden: false, hasVacancies: true, hasApplications: true,
  offices: [], vacancyCount: 2,
};
const role = {
  id: 11, name: 'Разработчик', stableKey: 'c05-dev', parentId: 10,
  path: 'IT → Разработка → Разработчик', sortOrder: 1,
  usageCount: 1, childCount: 0, isLeaf: true, descendantIds: [],
};
const root = {
  id: 10, name: 'Разработка', stableKey: 'c05-root', parentId: null,
  path: 'IT → Разработка', sortOrder: 1, usageCount: 1, childCount: 1,
  isLeaf: false, descendantIds: [11], children: [role],
};
const roles = { roles: [root, role], tree: [root], roots: [root], leaves: [role] };
const revisionA = 'a'.repeat(64);
const revisionB = 'b'.repeat(64);
const revisionC = 'c'.repeat(64);
const signedContext = 'ac1.' + 'A'.repeat(43);

function vacancy(id: number, title: string): VacancyDetail {
  return {
    id, companyName: company.name, entityType: 'company', realCompanyName: '',
    title, url: 'https://example.invalid/c05/' + id, descriptionMd: 'CI текст',
    role: '', roles: [role], level: 'Lead', salaryFrom: null, salaryTo: null,
    relevance: null, favorite: false, hidden: false, archived: false,
    publishedAt: '2026-11-20', loadedAt: '2026-11-20',
    createdAt: '2026-11-20 09:00:00', applicationId: null, applicationStatus: '',
    telegramSources: [], revision: revisionA, coverLetterSubject: '',
    coverLetterMd: '', coverLetterScope: 'shared',
  };
}
const vacancies = [
  vacancy(901, 'CI вакансия для отклика'),
  vacancy(902, 'CI соседняя вакансия'),
];

function application(id: number, vacancyId = 901): ApplicationRecord {
  return {
    id, revision: revisionA, companyName: company.name,
    vacancyId, vacancyTitle: vacancies.find((v) => v.id === vacancyId)?.title || '',
    status: 'Откликнулся', comment: 'CI исходный комментарий',
    recruiterName: 'CI рекрутер', recruiterPhone: '+79990000001',
    recruiterEmail: 'ci@example.invalid', recruiterContactType: 'direct',
    applicationSources: ['hh'],
    history: [{ fromStatus: '', toStatus: 'Откликнулся', changedAt: '2026-11-20' }],
  };
}
type Write = { path: string; method: string; user: string; body?: Record<string, unknown> };
type FixtureState = {
  user: string;
  records: Record<string, ApplicationRecord[]>;
  nextID: number;
  receipts: Map<string, number>;
  writes: Write[];
  createContextReads: number;
  failCreateOnce: boolean;
  conflictOnce: boolean;
  deleteFailure?: 'applied' | 'unapplied';
  events: string[];
};
async function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}
function mine(state: FixtureState) {
  return state.records[state.user] ??= [];
}
function response(state: FixtureState) {
  return { records: [...mine(state)], vacancies, entities: [company] };
}
function projectedVacancy(state: FixtureState, item: VacancyDetail) {
  const match = mine(state).find((r) => r.vacancyId === item.id);
  return {
    ...item, applicationId: match?.id ?? null, applicationStatus: match?.status ?? '',
  };
}
async function mount(page: Page, route = '/vacancies') {
  await page.goto(route);
  await page.addScriptTag({ content: script });
  if (route === '/vacancies')
    await expect(page.getByRole('heading', { name: 'Вакансии компаний' })).toBeVisible();
  if (route === '/applications')
    await expect(page.getByRole('heading', { name: 'Отклики', exact: true })).toBeVisible();
}
async function fixture(page: Page, options: {
  failCreateOnce?: boolean;
  conflictOnce?: boolean;
  seeded?: boolean;
  deleteFailure?: 'applied' | 'unapplied';
} = {}) {
  const state: FixtureState = {
    user: 'CI Alice', records: { 'CI Alice': options.seeded ? [application(1001)] : [], 'CI Bob': [] },
    nextID: 1002, receipts: new Map(), writes: [], createContextReads: 0,
    failCreateOnce: !!options.failCreateOnce, conflictOnce: !!options.conflictOnce,
    deleteFailure: options.deleteFailure, events: [],
  };
  await page.route('**/*', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const method = req.method();
    if (req.resourceType() === 'document') {
      return route.fulfill({
        status: 200, contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
    }
    if (path === '/api/session')
      return json(route, { name: state.user, isAdmin: true });
    if (path === '/api/vacancy-roles') return json(route, roles);
    if (path === '/api/channels') return json(route, { channels: [] });
    if (path === '/api/vacancies/list')
      return json(route, {
        vacancies: vacancies.map((v) => projectedVacancy(state, v)),
        roleTree: [root, role], entities: [company], applicationSummary: [],
      });
    if (/^\/api\/vacancies\/\d+$/.test(path) && method === 'GET') {
      const row = vacancies.find((v) => v.id === Number(path.split('/').at(-1)));
      return row
        ? json(route, { vacancy: projectedVacancy(state, row), entities: [company] })
        : json(route, { error: 'Не найдена' }, 404);
    }
    if (path === '/api/companies/detail')
      return json(route, { company });
    if (path === '/api/crm') return json(route, { status: '', comment: '' });
    if (path === '/api/applications/create-context') {
      state.createContextReads++;
      return json(route, { creationContext: signedContext });
    }
    if (path === '/api/crm/applications' || path === '/api/applications')
      return json(route, response(state));
    if (path === '/api/applications/create' && method === 'POST') {
      const body = req.postDataJSON() as Record<string, unknown>;
      state.writes.push({ path, method, user: state.user, body });
      if (body.creationContext !== signedContext || !/^[\da-f-]{36}$/.test(String(body.idempotencyKey)))
        return json(route, { error: 'CI неподтверждённое создание' }, 428);
      if (body.companyName !== company.name ||
          !vacancies.some((v) => v.id === body.vacancyId))
        return json(route, { error: 'CI неверный владелец или ID' }, 400);
      const receiptKey = state.user + ':' + body.idempotencyKey;
      let id = state.receipts.get(receiptKey);
      const replayed = !!id;
      if (!id) {
        id = state.nextID++;
        const item = application(id, Number(body.vacancyId));
        item.status = String(body.status);
        item.comment = String(body.comment);
        item.applicationSources = body.applicationSources as string[];
        state.records[state.user].push(item);
        state.receipts.set(receiptKey, id);
      }
      if (state.failCreateOnce) {
        state.failCreateOnce = false;
        return json(route, { error: 'CI ответ после COMMIT потерян' }, 503);
      }
      return json(route, { ...response(state), createdApplicationId: id, replayed }, replayed ? 200 : 201);
    }
    if (/^\/api\/applications\/\d+$/.test(path)) {
      const id = Number(path.split('/').at(-1));
      const item = mine(state).find((r) => r.id === id);
      if (method === 'GET') {
        state.events.push('GET ' + path);
        return item ? json(route, { record: item }) : json(route, { error: 'Не найден' }, 404);
      }
      if (method === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.writes.push({ path, method, user: state.user, body });
        if (!item) return json(route, { error: 'Не найден' }, 404);
        if (state.conflictOnce) {
          state.conflictOnce = false;
          item.comment = 'CI изменено другим сеансом';
          item.revision = revisionB;
          return json(route, { error: 'Конфликт версии' }, 409);
        }
        if (body.revision !== item.revision)
          return json(route, { error: 'Конфликт версии' }, 409);
        const beforeStatus = item.status;
        Object.assign(item, {
          status: body.status, comment: body.comment,
          recruiterName: body.recruiterName, recruiterPhone: body.recruiterPhone,
          recruiterEmail: body.recruiterEmail, recruiterContactType: body.recruiterContactType,
          applicationSources: body.applicationSources, revision: revisionC,
        });
        if (beforeStatus !== item.status)
          item.history.push({ fromStatus: beforeStatus, toStatus: item.status, changedAt: '2026-11-20' });
        return json(route, response(state));
      }
      if (method === 'DELETE') {
        state.writes.push({ path, method, user: state.user });
        state.events.push('DELETE ' + path);
        if (!item) return json(route, { error: 'Не найден' }, 404);
        const failed = state.deleteFailure;
        state.deleteFailure = undefined;
        if (failed === 'applied') {
          state.records[state.user] = mine(state).filter((r) => r.id !== id);
          return json(route, { error: 'CI ответ удаления потерян после COMMIT' }, 503);
        }
        if (failed === 'unapplied')
          return json(route, { error: 'CI удаление не было применено' }, 503);
        state.records[state.user] = mine(state).filter((r) => r.id !== id);
        return json(route, response(state));
      }
    }
    if (path.startsWith('/api/'))
      return json(route, { error: 'CI неожиданный маршрут ' + method + ' ' + path }, 500);
    await route.abort('blockedbyclient');
  });
  await mount(page);
  return state;
}
async function vacancyDrawer(page: Page, id = 901) {
  const item = vacancies.find((v) => v.id === id)!;
  await page.getByRole('button', { name: item.title, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: item.title, exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}
async function applicationTab(page: Page, id = 901) {
  const dialog = await vacancyDrawer(page, id);
  await dialog.getByRole('tab', { name: 'Отклик' }).click();
  await expect(dialog.getByRole('button', { name: 'Сохранить', exact: true })).toBeVisible();
  return dialog;
}

test('S11-B05-21/23: форма вакансии создаёт отклик с ID, после перечитывания он личный', async ({ page }) => {
  const state = await fixture(page);
  const dialog = await applicationTab(page);
  await dialog.getByRole('checkbox', { name: 'HH.ru' }).check();
  await dialog.getByRole('textbox', { name: 'Комментарий' }).fill('CI отклик связан с вакансией 901');
  const save = dialog.getByRole('button', { name: 'Сохранить', exact: true });
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog.getByRole('textbox', { name: 'Комментарий' }))
    .toHaveValue('CI отклик связан с вакансией 901');
  const post = state.writes.find((w) => w.method === 'POST');
  expect(post?.path).toBe('/api/applications/create');
  expect(post?.body?.vacancyId).toBe(901);
  expect(post?.body?.companyName).toBe(company.name);
  expect(post?.body?.applicationSources).toEqual(['hh']);
  expect(state.records['CI Alice']).toHaveLength(1);
  await mount(page, '/applications');
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(1);
  await expect(page.locator('.applications-table')).toContainText(vacancies[0].title);
  state.user = 'CI Bob';
  await mount(page, '/applications');
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
  expect(state.records['CI Bob']).toHaveLength(0);
});

test('S11-B05-24: после 503 с COMMIT повтор отправляет тот же receipt и не дублирует отклик', async ({ page }) => {
  const state = await fixture(page, { failCreateOnce: true });
  const dialog = await applicationTab(page);
  await dialog.getByRole('checkbox', { name: 'HH.ru' }).check();
  await dialog.getByRole('textbox', { name: 'Комментарий' }).fill('CI неизменный черновик');
  const save = dialog.getByRole('button', { name: 'Сохранить', exact: true });
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog.getByRole('alert')).toContainText('CI ответ после COMMIT потерян');
  expect(state.records['CI Alice']).toHaveLength(1);
  await expect(dialog.getByRole('textbox', { name: 'Комментарий' }))
    .toHaveValue('CI неизменный черновик');
  await save.click();
  await expect(dialog.getByRole('textbox', { name: 'Комментарий' }))
    .toHaveValue('CI неизменный черновик');
  const posts = state.writes.filter((w) => w.method === 'POST');
  expect(posts).toHaveLength(2);
  expect(posts[0].body?.idempotencyKey).toBe(posts[1].body?.idempotencyKey);
  expect(posts[0].body?.creationContext).toBe(posts[1].body?.creationContext);
  expect(state.createContextReads).toBe(1);
  expect(state.records['CI Alice']).toHaveLength(1);
});

test('S11-B05-25: редактирование комментария сохраняет прочие поля после повторного GET', async ({ page }) => {
  const state = await fixture(page, { seeded: true });
  await mount(page, '/applications');
  let row = page.locator('.applications-table tbody tr').first();
  await row.getByRole('button', { name: 'CI исходный комментарий' }).click();
  await row.getByRole('textbox', { name: 'Комментарий' }).fill('CI обновлённый комментарий');
  await row.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(row.getByText('CI обновлённый комментарий')).toBeVisible();
  const edit = state.writes.find((w) => w.method === 'PUT');
  expect(edit?.path).toBe('/api/applications/1001');
  expect(edit?.body?.revision).toBe(revisionA);
  expect(state.records['CI Alice'][0].recruiterName).toBe('CI рекрутер');
  await mount(page, '/applications');
  row = page.locator('.applications-table tbody tr').first();
  await expect(row.getByText('CI обновлённый комментарий')).toBeVisible();
  await expect(row).toContainText('CI рекрутер');
  await expect(row.getByRole('combobox', { name: 'Статус отклика ' + company.name }))
    .toHaveValue('Откликнулся');
});

test('S11-B05-26/27 (частично): 409 сохраняет черновик, согласование использует новую revision', async ({ page }) => {
  const state = await fixture(page, { seeded: true, conflictOnce: true });
  await mount(page, '/applications');
  const row = page.locator('.applications-table tbody tr').first();
  await row.getByRole('button', { name: 'CI исходный комментарий' }).click();
  await row.getByRole('textbox', { name: 'Комментарий' }).fill('CI мой черновик');
  await row.getByRole('button', { name: 'Сохранить', exact: true }).click();
  const conflict = row.getByRole('region', { name: 'Согласование изменений отклика' });
  await expect(conflict).toBeVisible();
  await expect(row.getByRole('textbox', { name: 'Комментарий' })).toHaveValue('CI мой черновик');
  await conflict.getByRole('button', { name: 'Загрузить актуальный отклик' }).click();
  await expect(conflict.getByText('CI изменено другим сеансом')).toBeVisible();
  await conflict.getByRole('combobox', { name: 'Согласовать: Комментарий' })
    .selectOption('draft');
  await conflict.getByRole('button', { name: 'Применить к форме' }).click();
  await expect(row.getByRole('textbox', { name: 'Комментарий' })).toHaveValue('CI мой черновик');
  await row.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(row.getByText('CI мой черновик')).toBeVisible();
  const revisions = state.writes.filter((w) => w.method === 'PUT').map((w) => w.body?.revision);
  expect(revisions).toEqual([revisionA, revisionB]);
  expect(state.records['CI Alice'][0].comment).toBe('CI мой черновик');
});

test('S11-B05-28/29: отмена удаления не отправляет DELETE, подтверждение удаляет запись', async ({ page }) => {
  const state = await fixture(page, { seeded: true });
  await mount(page, '/applications');
  const row = page.locator('.applications-table tbody tr').first();
  const remove = row.getByRole('button', { name: 'Удалить отклик ' + vacancies[0].title });
  page.once('dialog', (dialog) => dialog.dismiss());
  await remove.click();
  expect(state.writes.filter((w) => w.method === 'DELETE')).toHaveLength(0);
  await expect(row).toBeVisible();
  page.once('dialog', (dialog) => dialog.accept());
  await remove.click();
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
  expect(state.writes.filter((w) => w.method === 'DELETE')).toHaveLength(1);
  expect(state.records['CI Alice']).toHaveLength(0);
  await mount(page, '/applications');
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
});


test('S11-B05-22: компания сохраняет выбранную вакансию при отмене перехода на другую вкладку', async ({ page }) => {
  const state = await fixture(page);
  await page.locator('.vacancies-company').first().click();
  const companyDialog = page.getByRole('dialog', { name: company.name });
  await expect(companyDialog).toBeVisible();
  await companyDialog.getByRole('tab', { name: 'Отклики' }).click();
  await companyDialog.getByRole('button', { name: 'Новый отклик' }).click();
  const vacancySelect = companyDialog.getByRole('combobox', { name: 'Вакансия' });
  await vacancySelect.selectOption('902');
  await companyDialog.getByRole('checkbox', { name: 'HH.ru' }).check();
  await expect(vacancySelect).toHaveValue('902');
  page.once('dialog', (confirmation) => confirmation.dismiss());
  await companyDialog.getByRole('tab', { name: 'Вакансии' }).click();
  await expect(companyDialog.getByRole('tab', { name: 'Отклики' }))
    .toHaveAttribute('aria-selected', 'true');
  await expect(vacancySelect).toHaveValue('902');
  await companyDialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect.poll(() => state.writes.filter((w) => w.method === 'POST').length).toBe(1);
  expect(state.writes.find((w) => w.method === 'POST')?.body?.vacancyId).toBe(902);
  expect(state.records['CI Alice'][0].vacancyId).toBe(902);
});

test('S11-B05-30: после 503 с COMMIT повтор подтверждает отсутствие через GET, не посылая второй DELETE', async ({ page }) => {
  const state = await fixture(page, { seeded: true, deleteFailure: 'applied' });
  await mount(page, '/applications');
  const row = page.locator('.applications-table tbody tr').first();
  const remove = row.getByRole('button', { name: 'Удалить отклик ' + vacancies[0].title });
  page.once('dialog', (confirmation) => confirmation.accept());
  await remove.click();
  await expect(row.getByRole('alert')).toContainText('CI ответ удаления потерян после COMMIT');
  expect(state.records['CI Alice']).toHaveLength(0);
  await expect(row).toBeVisible();
  page.once('dialog', (confirmation) => confirmation.accept());
  await remove.click();
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
  expect(state.events).toEqual([
    'DELETE /api/applications/1001',
    'GET /api/applications/1001',
  ]);
  expect(state.writes.filter((w) => w.method === 'DELETE')).toHaveLength(1);
  await mount(page, '/applications');
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
});

test('S11-B05-30: если DELETE не применён, повторный GET проверяет запись до нового DELETE', async ({ page }) => {
  const state = await fixture(page, { seeded: true, deleteFailure: 'unapplied' });
  await mount(page, '/applications');
  const row = page.locator('.applications-table tbody tr').first();
  const remove = row.getByRole('button', { name: 'Удалить отклик ' + vacancies[0].title });
  page.once('dialog', (confirmation) => confirmation.accept());
  await remove.click();
  await expect(row.getByRole('alert')).toContainText('CI удаление не было применено');
  expect(state.records['CI Alice']).toHaveLength(1);
  page.once('dialog', (confirmation) => confirmation.accept());
  await remove.click();
  await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
  expect(state.events).toEqual([
    'DELETE /api/applications/1001',
    'GET /api/applications/1001',
    'DELETE /api/applications/1001',
  ]);
  expect(state.records['CI Alice']).toHaveLength(0);
});
