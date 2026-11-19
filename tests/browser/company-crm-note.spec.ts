import { test, expect, type Page } from '@playwright/test';
import { build } from 'esbuild';

// Проверяются реальные React-формы и API-client; сеть перехвачена синтетически.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'company-crm-fixture.tsx',
    resolveDir: process.cwd(),
    loader: 'tsx',
  },
  bundle: true,
  write: false,
  platform: 'browser',
  format: 'iife',
  jsx: 'automatic',
  plugins: [
    {
      name: 'empty-test-css',
      setup(compiler) {
        compiler.onResolve({ filter: /\.css$/ }, (args) => ({
          path: args.path,
          namespace: 'test-css',
        }));
        compiler.onLoad({ filter: /.*/, namespace: 'test-css' }, () => ({
          contents: '',
          loader: 'js',
        }));
      },
    },
  ],
});
const script = bundle.outputFiles[0].text;
const company = 'CI компания CRM';
type Change = { fromStatus: string; toStatus: string; changedAt: string };
type State = { status: string; comment: string; history: Change[] };
type TestModel = {
  user: 'Alpha' | 'Beta';
  users: Record<'Alpha' | 'Beta', State>;
  writes: { user: string; path: string; body: Record<string, unknown> }[];
  nextFailure?: 'lost_ack' | 'reject';
  holdNextWrite?: Promise<void>;
};

function model(): TestModel {
  const blank = (): State => ({ status: '', comment: '', history: [] });
  return { user: 'Alpha', users: { Alpha: blank(), Beta: blank() }, writes: [] };
}

function entity() {
  return {
    id: 1,
    name: company,
    entityType: 'company',
    rank: 2,
    favorite: false,
    hidden: false,
    offices: [],
    rating: null,
    salaryRangeSource: {},
    middle: [],
    senior: [],
    lead: [],
    head: [],
  };
}

async function fixture(page: Page, data: TestModel) {
  await page.route('**/*', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    const json = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify(body),
      });
    if (req.resourceType() === 'document')
      return route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
    if (path === '/api/session') return json({ name: data.user, isAdmin: false });
    if (path === '/api/tasks') return json({ tasks: [], entities: [] });
    if (path === '/api/google-calendar/status')
      return json({ configured: false, connected: false });
    if (path === '/api/companies/list') return json({ companies: [entity()] });
    if (path === '/api/companies/detail') return json({ company: entity() });
    if (path === '/api/applications') return json({ records: [], vacancies: [] });
    if (path === '/api/crm') {
      if (url.searchParams.get('company') !== company)
        return json({ error: 'Имя компании должно быть в query' }, 400);
      const user = data.user;
      if (req.method() === 'GET') return json(data.users[user]);
      if (req.method() !== 'PUT') return json({ error: 'Unsupported method' }, 405);
      const body = req.postDataJSON() as Record<string, unknown>;
      data.writes.push({ user, path: url.pathname + url.search, body });
      if (
        Object.keys(body).sort().join(',') !== 'comment,status' ||
        typeof body.status !== 'string' ||
        typeof body.comment !== 'string'
      )
        return json({ error: 'Нарушен Go PUT /api/crm контракт' }, 400);
      const hold = data.holdNextWrite;
      data.holdNextWrite = undefined;
      if (hold) await hold;
      const mode = data.nextFailure;
      data.nextFailure = undefined;
      if (mode !== 'reject') {
        const current = data.users[user];
        if (current.status !== body.status)
          current.history.unshift({
            fromStatus: current.status,
            toStatus: body.status,
            changedAt: '2026-11-19T00:00:00Z',
          });
        current.status = body.status;
        current.comment = body.comment;
      }
      if (mode) return json({ error: 'CI synthetic response unavailable' }, 503);
      return json(data.users[user]);
    }
    if (path.startsWith('/api/'))
      return json({ error: 'Unexpected API request: ' + path }, 500);
    return route.abort('blockedbyclient');
  });
  await page.goto('/__ci_company_crm_fixture');
  return open(page);
}

async function open(page: Page) {
  await page.addScriptTag({ content: script });
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await page.getByRole('button', { name: company, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: company, exact: true });
  await dialog.getByRole('tab', { name: 'Отклики' }).click();
  await expect(dialog.getByRole('combobox', { name: 'Статус компании' })).toBeVisible();
  return dialog;
}

test('S11-B03-11/14/15/16: comment/status roundtrip через Go контракт, одно событие истории', async ({
  page,
}) => {
  const data = model();
  let dialog = await fixture(page, data);
  const status = dialog.getByRole('combobox', { name: 'Статус компании' });
  const comment = dialog.getByRole('textbox', { name: 'Комментарий' });
  await comment.fill('Синтетический комментарий');
  await status.selectOption('Откликнулся');
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(dialog.getByRole('status', { name: '' })).toContainText('Сохранено');
  expect(data.writes).toHaveLength(1);
  expect(data.writes[0].path).toBe('/api/crm?company=' + encodeURIComponent(company));
  expect(data.writes[0].body).toEqual({
    status: 'Откликнулся',
    comment: 'Синтетический комментарий',
  });
  await page.reload();
  dialog = await open(page);
  await expect(dialog.getByRole('combobox', { name: 'Статус компании' })).toHaveValue(
    'Откликнулся',
  );
  await expect(dialog.getByRole('textbox', { name: 'Комментарий' })).toHaveValue(
    'Синтетический комментарий',
  );
  await expect(dialog.getByRole('list', { name: 'История статусов компании' }).locator('li')).toHaveCount(1);
  expect(data.users.Alpha.history).toHaveLength(1);
});

test('S11-B03-12/13/20: отказ сохраняет draft, повтор после lost ACK сначала делает GET', async ({
  page,
}) => {
  const data = model();
  data.nextFailure = 'lost_ack';
  const dialog = await fixture(page, data);
  const status = dialog.getByRole('combobox', { name: 'Статус компании' });
  const comment = dialog.getByRole('textbox', { name: 'Комментарий' });
  await status.selectOption('Скрининг');
  await comment.fill('Черновик после потерянного ответа');
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(comment).toHaveValue('Черновик после потерянного ответа');
  expect(data.users.Alpha.comment).toBe('Черновик после потерянного ответа');
  expect(data.writes).toHaveLength(1);
  // Результат неизвестен: повтор не должен создавать второй PUT и событие истории.
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(dialog.getByText('Сохранено', { exact: true })).toBeVisible();
  expect(data.writes).toHaveLength(1);
  expect(data.users.Alpha.history).toHaveLength(1);
  // Пустой комментарий считается сохранённым только после подтверждённого PUT.
  await comment.fill('');
  expect(data.users.Alpha.comment).not.toBe('');
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(dialog.getByText('Сохранено', { exact: true })).toBeVisible();
  expect(data.users.Alpha.comment).toBe('');
  expect(data.users.Alpha.status).toBe('Скрининг');
  expect(data.users.Alpha.history).toHaveLength(1);
});

test('S11-B03-13/18/19: отклонённая запись не теряет текст, form guard не позволяет уйти без подтверждения', async ({
  page,
}) => {
  const data = model();
  data.nextFailure = 'reject';
  const dialog = await fixture(page, data);
  const comment = dialog.getByRole('textbox', { name: 'Комментарий' });
  await comment.fill('Несохранённая заметка');
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(comment).toHaveValue('Несохранённая заметка');
  expect(data.users.Alpha.comment).toBe('');
  page.once('dialog', async (confirmation) => confirmation.dismiss());
  await dialog.getByRole('tab', { name: 'Работодатель' }).click();
  await expect(comment).toBeVisible();
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(dialog.getByText('Сохранено', { exact: true })).toBeVisible();
  expect(data.users.Alpha.comment).toBe('Несохранённая заметка');
});

test('S11-B03-17: два синтетических пользователя имеют разную историю и комментарий', async ({
  page,
}) => {
  const data = model();
  let dialog = await fixture(page, data);
  await dialog.getByRole('combobox', { name: 'Статус компании' }).selectOption('Откликнулся');
  await dialog.getByRole('textbox', { name: 'Комментарий' }).fill('Личная заметка A');
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(dialog.getByText('Сохранено', { exact: true })).toBeVisible();
  data.user = 'Beta';
  await page.reload();
  dialog = await open(page);
  await expect(dialog.getByRole('combobox', { name: 'Статус компании' })).toHaveValue('');
  await expect(dialog.getByRole('textbox', { name: 'Комментарий' })).toHaveValue('');
  await expect(dialog.getByRole('list', { name: 'История статусов компании' })).toHaveCount(0);
  expect(data.users.Alpha.history).toHaveLength(1);
  expect(data.users.Beta.history).toHaveLength(0);
});
