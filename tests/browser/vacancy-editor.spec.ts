import { test, expect, type Page, type Route } from '@playwright/test';
import { build } from 'esbuild';
import type { VacancyDetail } from '../../shared/contracts';

// Синтетические HTTP-ответы и настоящие React-компоненты: без production и платных API.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'vacancy-editor-fixture.tsx',
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
  id: 1, name: 'CI работодатель B05', entityType: 'company',
  rank: 1, favorite: false, hidden: false, hasVacancies: true,
  offices: [], vacancyCount: 1,
};
const leaf = {
  id: 11, stableKey: 'backend-b05', name: 'Разработчик', parentId: 10,
  path: 'IT → Разработка → Разработчик', sortOrder: 1,
  usageCount: 1, childCount: 0, isLeaf: true, descendantIds: [],
};
const root = {
  id: 10, stableKey: 'engineering-b05', name: 'Разработка', parentId: null,
  path: 'IT → Разработка', sortOrder: 1, usageCount: 1,
  childCount: 1, isLeaf: false, descendantIds: [11], children: [leaf],
};
const roles = { roles: [root, leaf], tree: [root], roots: [root], leaves: [leaf] };

function vacancy(id: number, title: string, description = 'CI описание'): VacancyDetail {
  return {
    id, companyName: company.name, entityType: 'company', realCompanyName: '',
    title, url: 'https://example.invalid/b05/' + id,
    descriptionMd: description, role: '', roles: [leaf], level: 'Lead',
    relevance: null, salaryFrom: null, salaryTo: null, favorite: false,
    hidden: false, archived: false, publishedAt: '2026-11-20',
    loadedAt: '2026-11-20', createdAt: '2026-11-20 09:00:00',
    applicationId: null, applicationStatus: '', telegramSources: [],
    revision: 'ci-revision-1', coverLetterSubject: '', coverLetterMd: '',
    coverLetterScope: 'shared',
  };
}

type Write = { path: string; method: string; body: Record<string, unknown> };
type FixtureState = {
  rows: VacancyDetail[];
  writes: Write[];
  reads: string[];
  conflictOnce: boolean;
};
async function respond(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}

async function fixture(page: Page, options: { conflictOnce?: boolean } = {}) {
  const state: FixtureState = {
    rows: [vacancy(901, 'CI существующая вакансия')],
    writes: [], reads: [], conflictOnce: !!options.conflictOnce,
  };
  await page.route('**/*', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    const method = req.method();
    if (req.resourceType() === 'document') {
      return route.fulfill({
        status: 200, contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
    }
    if (path === '/api/session') return respond(route, { name: 'CI Editor', isAdmin: true });
    if (path === '/api/vacancy-roles') return respond(route, roles);
    if (path === '/api/channels') return respond(route, { channels: [] });
    if (path === '/api/vacancies/list' || path === '/api/vacancies') {
      if (method === 'GET') {
        state.reads.push(path);
        return respond(route, {
          vacancies: state.rows, roleTree: [root, leaf], entities: [company],
          applicationSummary: [],
        });
      }
      if (path === '/api/vacancies' && method === 'POST') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.writes.push({ path, method, body });
        const created: VacancyDetail = {
          ...vacancy(902, String(body.title), String(body.descriptionMd)),
          url: String(body.url),
          roles: Array.isArray(body.roleIds) && body.roleIds.includes(leaf.id) ? [leaf] : [],
          level: String(body.level),
        };
        state.rows.push(created);
        return respond(route, { vacancies: state.rows });
      }
    }
    if (path === '/api/companies/detail')
      return respond(route, { company: { ...company, vacancyCount: state.rows.length } });
    if (/^\/api\/vacancies\/\d+$/.test(path)) {
      const id = Number(path.split('/').at(-1));
      const row = state.rows.find((v) => v.id === id);
      if (!row) return respond(route, { error: 'Вакансия не найдена' }, 404);
      if (method === 'GET') {
        state.reads.push(path);
        return respond(route, { vacancy: row, entities: [company] });
      }
      if (method === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.writes.push({ path, method, body });
        if (state.conflictOnce) {
          state.conflictOnce = false;
          const current = { ...row, title: 'CI версия на сервере', revision: 'ci-revision-2' };
          state.rows = state.rows.map((v) => v.id === id ? current : v);
          return respond(route, { error: 'Вакансия изменилась' }, 409);
        }
        if (body.revision !== row.revision)
          return respond(route, { error: 'Устаревшая версия' }, 409);
        const updated = {
          ...row, title: String(body.title), descriptionMd: String(body.descriptionMd),
          revision: 'ci-revision-2',
        };
        state.rows = state.rows.map((v) => v.id === id ? updated : v);
        return respond(route, { vacancies: state.rows });
      }
    }
    if (path.startsWith('/api/'))
      return respond(route, { error: 'Неожиданный синтетический вызов: ' + path }, 500);
    await route.abort('blockedbyclient');
  });
  await page.goto('/vacancies');
  await page.addScriptTag({ content: script });
  await expect(page.getByRole('heading', { name: 'Вакансии компаний' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'CI существующая вакансия', exact: true })).toBeVisible();
  return state;
}

async function openCreate(page: Page) {
  await page.locator('.vacancies-company').first().click();
  const companyDialog = page.getByRole('dialog', { name: company.name });
  await expect(companyDialog).toBeVisible();
  await companyDialog.getByRole('tab', { name: 'Вакансии' }).click();
  await companyDialog.getByRole('button', { name: 'Добавить вакансию' }).click();
  const editor = page.getByRole('dialog', { name: 'Новая вакансия' });
  await expect(editor).toBeVisible();
  return editor;
}

async function openEdit(page: Page) {
  await page.getByRole('button', { name: 'CI существующая вакансия', exact: true }).click();
  const detail = page.getByRole('dialog', { name: 'CI существующая вакансия' });
  await expect(detail).toBeVisible();
  await detail.getByRole('button', { name: 'Редактировать' }).click();
  const editor = page.getByRole('dialog', { name: 'Редактирование вакансии' });
  await expect(editor).toBeVisible();
  return editor;
}

async function fillCreate(editor: import('@playwright/test').Locator) {
  await editor.getByRole('textbox', { name: 'Название вакансии *' }).fill('CI новая вакансия');
  await editor.getByRole('textbox', { name: 'Ссылка на вакансию *' })
    .fill('https://example.invalid/b05/902?source=ci');
  await editor.getByRole('combobox', { name: 'Уровень *' }).selectOption('Lead');
  await editor.getByRole('textbox', { name: 'Полный текст вакансии' })
    .fill('**CI сохранённое описание**');
}

test('S11-B05-01/02: создание из компании открывает карточку с ID, описанием и ролью', async ({ page }) => {
  const state = await fixture(page);
  const editor = await openCreate(page);
  await fillCreate(editor);
  await editor.getByRole('checkbox', { name: 'Разработчик', exact: true }).check();
  await editor.getByRole('button', { name: 'Добавить вакансию' }).click();
  const detail = page.getByRole('dialog', { name: 'CI новая вакансия' });
  await expect(detail).toBeVisible();
  await expect(detail.getByText('CI сохранённое описание')).toBeVisible();
  await expect(detail.getByText(leaf.path, { exact: true }).first()).toBeVisible();
  expect(state.writes.map((w) => w.method + ' ' + w.path)).toEqual(['POST /api/vacancies']);
  expect(state.writes[0].body.roleIds).toEqual([leaf.id]);
  expect(state.reads).toContain('/api/vacancies/902');
  await detail.getByRole('button', { name: 'Закрыть' }).click();
  await expect(page.getByRole('button', { name: 'CI новая вакансия', exact: true })).toBeVisible();
});

test('S11-B05-03/04: ошибка роли не отправляет POST, черновик сохраняется после исправления', async ({ page }) => {
  const state = await fixture(page);
  const editor = await openCreate(page);
  await fillCreate(editor);
  await editor.getByRole('button', { name: 'Добавить вакансию' }).click();
  await expect(editor.getByRole('alert')).toContainText('Укажите хотя бы одну роль');
  expect(state.writes).toHaveLength(0);
  await expect(editor.getByRole('textbox', { name: 'Название вакансии *' }))
    .toHaveValue('CI новая вакансия');
  await expect(editor.getByRole('textbox', { name: 'Полный текст вакансии' }))
    .toHaveValue('**CI сохранённое описание**');
  await editor.getByRole('checkbox', { name: 'Разработчик', exact: true }).check();
  await editor.getByRole('button', { name: 'Добавить вакансию' }).click();
  await expect(page.getByRole('dialog', { name: 'CI новая вакансия' })).toBeVisible();
  expect(state.writes.map((w) => w.method)).toEqual(['POST']);
});

test('S11-B05-05: PUT с исходной revision обновляет карточку и список', async ({ page }) => {
  const state = await fixture(page);
  const editor = await openEdit(page);
  await editor.getByRole('textbox', { name: 'Название вакансии *' })
    .fill('CI отредактированная вакансия');
  await editor.getByRole('textbox', { name: 'Полный текст вакансии' })
    .fill('CI новое описание после PUT');
  await editor.getByRole('button', { name: 'Сохранить изменения' }).click();
  const detail = page.getByRole('dialog', { name: 'CI отредактированная вакансия' });
  await expect(detail).toBeVisible();
  await expect(detail.getByText('CI новое описание после PUT')).toBeVisible();
  expect(state.writes.map((w) => w.method + ' ' + w.path))
    .toEqual(['PUT /api/vacancies/901']);
  expect(state.writes[0].body.revision).toBe('ci-revision-1');
  expect(state.rows[0].revision).toBe('ci-revision-2');
  await detail.getByRole('button', { name: 'Закрыть' }).click();
  await expect(page.getByRole('button', { name: 'CI отредактированная вакансия', exact: true }))
    .toBeVisible();
  await expect(page.getByRole('button', { name: 'CI существующая вакансия', exact: true }))
    .toHaveCount(0);
});

test('S11-B05-06/07 (частично): 409 сохраняет черновик и отмена выхода не пишет повторно', async ({ page }) => {
  const state = await fixture(page, { conflictOnce: true });
  const editor = await openEdit(page);
  await editor.getByRole('textbox', { name: 'Название вакансии *' })
    .fill('CI мой несохранённый черновик');
  await editor.getByRole('button', { name: 'Сохранить изменения' }).click();
  const conflict = editor.getByRole('region', { name: 'Конфликт изменений вакансии' });
  await expect(conflict).toBeVisible();
  await expect(editor.getByRole('textbox', { name: 'Название вакансии *' }))
    .toHaveValue('CI мой несохранённый черновик');
  await conflict.getByRole('button', { name: 'Показать актуальную версию' }).click();
  await expect(conflict.getByRole('region', { name: 'Актуальная версия вакансии' }))
    .toContainText('CI версия на сервере');
  await expect(editor.getByRole('textbox', { name: 'Название вакансии *' }))
    .toHaveValue('CI мой несохранённый черновик');
  page.once('dialog', (dialog) => dialog.dismiss());
  await editor.getByRole('button', { name: 'Отмена' }).click();
  await expect(editor).toBeVisible();
  expect(state.writes.map((w) => w.method)).toEqual(['PUT']);
  expect(state.rows[0].title).toBe('CI версия на сервере');
});
