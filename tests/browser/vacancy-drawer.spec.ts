import { test, expect, type Page, type Route } from '@playwright/test';
import { build } from 'esbuild';

// Настоящие React, Drawer и API-client. HTTP полностью синтетический; без платных вызовов.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'vacancy-drawer-fixture.tsx',
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

const employer = {
  name: 'CI работодатель B04', entityType: 'company', rank: 1,
  favorite: false, hidden: false, hasVacancies: true, offices: [],
};
const engineering = {
  id: 11, stableKey: 'backend', name: 'Разработчик', parentId: 10,
  path: 'IT → Разработка → Разработчик', sortOrder: 1,
  usageCount: 1, childCount: 0, isLeaf: true, descendantIds: [],
};
const product = {
  ...engineering, id: 21, stableKey: 'productdev', parentId: 20,
  path: 'IT → Продукт → Разработчик',
};
const retired = {
  ...engineering, id: 31, stableKey: 'retired', parentId: 30,
  path: 'Архив → Прежнее направление → Разработчик',
};
const roots = [
  {
    id: 10, stableKey: 'engineering', name: 'Разработка', parentId: null,
    path: 'IT → Разработка', sortOrder: 1,
    usageCount: 1, childCount: 1, isLeaf: false, descendantIds: [11],
    children: [engineering],
  },
  {
    id: 20, stableKey: 'product', name: 'Продукт', parentId: null,
    path: 'IT → Продукт', sortOrder: 2,
    usageCount: 1, childCount: 1, isLeaf: false, descendantIds: [21],
    children: [product],
  },
];
const catalog = {
  roles: [...roots, engineering, product], tree: roots,
  roots, leaves: [engineering, product],
};
function record(id: number, title: string, assigned = [engineering], description = 'Текст вакансии') {
  return {
    id, companyName: employer.name, entityType: 'company',
    realCompanyName: '', title, salaryFrom: null, salaryTo: null,
    role: '', roles: assigned, level: 'Lead', relevance: null,
    favorite: false, hidden: false, archived: false,
    publishedAt: '2026-11-19', loadedAt: null,
    createdAt: '2026-11-19 10:00:00',
    applicationId: null, applicationStatus: '', telegramSources: [],
    revision: 'ci-revision', url: 'https://example.invalid/vacancy/' + id,
    descriptionMd: description, coverLetterSubject: '', coverLetterMd: '',
    coverLetterScope: 'shared',
  };
}
type Data = ReturnType<typeof record>;
type DetailResolver = (id: number) => Promise<Data | null> | Data | null;
type FixtureOptions = {
  records: Data[];
  detail?: DetailResolver;
  tasksFailure?: boolean;
  roleResponse?: () => Promise<unknown> | unknown;
  admin?: boolean;
};
async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}
async function fixture(page: Page, options: FixtureOptions) {
  await page.route('**/*', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (req.resourceType() === 'document') {
      await route.fulfill({
        status: 200, contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
      return;
    }
    if (path === '/api/session')
      return json(route, { name: 'CI Drawer', isAdmin: options.admin || false });
    if (path === '/api/vacancies/list')
      return json(route, {
        vacancies: options.records, entities: [employer], applicationSummary: [],
        roleTree: [...roots, engineering, product],
      });
    if (path === '/api/vacancy-roles')
      return json(route, options.roleResponse ? await options.roleResponse() : catalog);
    if (path === '/api/channels') return json(route, { channels: [] });
    if (path === '/api/tasks' && options.tasksFailure)
      return json(route, { error: 'CI задачи недоступны' }, 503);
    if (path === '/api/tasks')
      return json(route, { tasks: [], entities: [] });
    if (/^\/api\/vacancies\/\d+$/.test(path)) {
      const id = Number(path.split('/').at(-1));
      const value = options.detail ? await options.detail(id) : options.records.find((r) => r.id === id) ?? null;
      if (!value) return json(route, { error: 'Вакансия не найдена' }, 404);
      return json(route, { vacancy: value, entities: [employer] });
    }
    if (path.startsWith('/api/'))
      return json(route, { error: 'Unexpected request ' + path }, 500);
    await route.abort('blockedbyclient');
  });
  await page.goto('/vacancies');
  await page.addScriptTag({ content: script });
  await expect(page.getByRole('heading', { name: 'Вакансии компаний' })).toBeVisible();
}
async function open(page: Page, title: string) {
  await page.getByRole('button', { name: title, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: title, exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('S11-B04-11/12/20: пустое описание, 404 без предыдущей карточки и возврат фокуса', async ({ page }) => {
  const first = record(101, 'CI первая карточка', [engineering], 'CI старое описание');
  const second = record(102, 'CI пустое описание', [], '');
  const missing = record(103, 'CI неизвестная карточка');
  await fixture(page, {
    records: [first, second, missing],
    detail: (id) => id === 103 ? null : [first, second].find((r) => r.id === id) ?? null,
  });
  let dialog = await open(page, first.title);
  await expect(dialog.getByText('CI старое описание')).toBeVisible();
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  await expect(dialog).toHaveCount(0);
  dialog = await open(page, second.title);
  await expect(dialog.getByText('Полный текст пока не добавлен.')).toBeVisible();
  await expect(dialog.getByText('CI старое описание')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  const opener = page.getByRole('button', { name: missing.title, exact: true });
  await opener.click();
  const errorDialog = page.getByRole('dialog', { name: 'Вакансия недоступна' });
  await expect(errorDialog.getByRole('alert')).toContainText('Вакансия не найдена');
  await expect(errorDialog.getByText('CI старое описание')).toHaveCount(0);
  await errorDialog.getByRole('button', { name: 'Закрыть' }).click();
  await expect(errorDialog).toHaveCount(0);
  await expect(opener).toBeFocused();
});

test('S11-B04-13: поздний GET закрытой карточки не подменяет следующую', async ({ page }) => {
  const first = record(201, 'CI запоздавшая карточка', [engineering], 'CI текст старой');
  const second = record(202, 'CI новая карточка', [product], 'CI текст новой');
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await fixture(page, {
    records: [first, second],
    detail: async (id) => {
      if (id === first.id) await gate;
      return id === first.id ? first : second;
    },
  });
  await page.getByRole('button', { name: first.title, exact: true }).click();
  const pending = page.getByRole('dialog', { name: 'Загрузка вакансии…' });
  await expect(pending).toBeVisible();
  await pending.getByRole('button', { name: 'Закрыть' }).click();
  const dialog = await open(page, second.title);
  await expect(dialog.getByText('CI текст новой')).toBeVisible();
  release();
  await expect(dialog.getByText('CI текст старой')).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: first.title })).toHaveCount(0);
});

test('S11-B04-14: отказ загрузки задач не скрывает остальные вкладки карточки', async ({ page }) => {
  const v = record(301, 'CI вкладки карточки', [engineering], 'CI описание доступно');
  await fixture(page, { records: [v], tasksFailure: true });
  const dialog = await open(page, v.title);
  await dialog.getByRole('tab', { name: 'Задачи' }).click();
  await expect(dialog.getByRole('alert')).toContainText('CI задачи недоступны');
  await expect(dialog.getByRole('tab', { name: 'Описание' })).toBeVisible();
  await expect(dialog.getByRole('tab', { name: 'Инфо' })).toBeVisible();
  await dialog.getByRole('tab', { name: 'Описание' }).click();
  await expect(dialog.getByText('CI описание доступно')).toBeVisible();
});

test('S11-B04-15/16/17: одинаковые имена различаются путями, сохранённая архивная роль видна', async ({ page }) => {
  const engineeringVacancy = record(401, 'CI разработка', [engineering]);
  const productVacancy = record(402, 'CI продукт', [product]);
  const retiredVacancy = record(403, 'CI прежняя роль', [retired]);
  await fixture(page, { records: [engineeringVacancy, productVacancy, retiredVacancy] });
  let dialog = await open(page, engineeringVacancy.title);
  await expect(dialog.getByText(engineering.path)).toBeVisible();
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  dialog = await open(page, productVacancy.title);
  await expect(dialog.getByText(product.path)).toBeVisible();
  await expect(dialog.getByText(engineering.path)).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  dialog = await open(page, retiredVacancy.title);
  await expect(dialog.getByText(retired.path)).toBeVisible();
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  await page.getByRole('button', { name: /Роль · Все/ }).click();
  await expect(page.getByRole('checkbox', { name: retired.name, exact: true })).toHaveCount(2);
  // В дереве выбора присутствуют только две действующие роли с этим именем.
});

test('S11-B04-18: загрузка дерева после клавиатурного открытия не теряет фокус', async ({ page }) => {
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const v = record(501, 'CI клавиатура дерева');
  await fixture(page, {
    records: [v],
    roleResponse: async () => { await gate; return catalog; },
  });
  const trigger = page.getByRole('button', { name: /Роль · Все/ });
  await trigger.focus();
  await page.keyboard.press('Enter');
  await expect(trigger).toBeFocused();
  await expect(page.getByText('Загрузка ролей…')).toBeVisible();
  release();
  await expect(page.getByRole('checkbox', { name: 'Разработка' })).toBeVisible();
  await expect(trigger).toBeFocused();
});

test('S11-B04-19: после второй карточки с пустыми ролями старый путь не остаётся', async ({ page }) => {
  const first = record(601, 'CI роль назначена', [engineering]);
  const second = record(602, 'CI роль отсутствует', [], '');
  await fixture(page, { records: [first, second] });
  let dialog = await open(page, first.title);
  await expect(dialog.getByText(engineering.path)).toBeVisible();
  await dialog.getByRole('button', { name: 'Закрыть' }).click();
  dialog = await open(page, second.title);
  await expect(dialog.getByText(engineering.path)).toHaveCount(0);
  await expect(dialog.getByText('Полный текст пока не добавлен.')).toBeVisible();
});
