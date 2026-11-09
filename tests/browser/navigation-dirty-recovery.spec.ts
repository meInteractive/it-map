import {test as browserTest, expect, type Page} from './component-fixtures.js';
import { build } from 'esbuild';

// Реальная навигация Workspace и реальный VacancyEditor в Chromium, только фиктивные API.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "const root = createRoot(document.getElementById('root'));",
      "root.render(<Application />);",
      "window.__unmountApplication = () => root.unmount();",
    ].join('\n'),
    sourcefile: 'navigation-dirty-fixture.tsx', resolveDir: process.cwd(), loader: 'tsx',
  },
  bundle: true, write: false, platform: 'browser', format: 'iife', jsx: 'automatic',
  plugins: [{
    name: 'empty-test-css',
    setup(build) {
      build.onResolve({ filter: /\.css$/ }, args => ({ path: args.path, namespace: 'test-css' }));
      build.onLoad({ filter: /.*/, namespace: 'test-css' }, () => ({ contents: '', loader: 'js' }));
    },
  }],
});
const source = bundle.outputFiles[0].text;


// Real Workspace + VacancyEditor, each test gets a fresh browser Page and
// isolated synthetic data. No server, real user data, paid calls or persistent DB.
async function mountDirtyWorkspace(page: Page) {
  // A local synthetic same-origin path permits sessionStorage and normal
  // navigation, without changing the production content security policy.
  const fixturePath = '/__ci_navigation_dirty_fixture';
  await page.route('**/*', route => {
    if (new URL(route.request().url()).pathname === fixturePath)
      return route.fulfill({status: 200, contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>'});
    return route.abort('blockedbyclient');
  });
  await page.goto(fixturePath);
  await page.evaluate(() => {
    (window as any).__confirmAnswers = [];
    (window as any).__confirmMessages = [];
    window.confirm = message => {
      (window as any).__confirmMessages.push(message);
      return (window as any).__confirmAnswers.shift() ?? false;
    };
    (window as any).__writes = [];
    let record = {
      id: 71, revision: 'a'.repeat(64), title: 'Синтетическая вакансия',
      companyName: 'Синтетический работодатель', entityType: 'company',
      url: 'https://example.test/71', role: 'Менеджер проектов', roles: [],
      level: 'Senior', descriptionMd: '## Полный текст',
      coverLetterSubject: '', coverLetterMd: '', coverLetterScope: 'shared',
      salaryFrom: null, salaryTo: null, relevance: 0, favorite: false, hidden: false,
      telegramSources: [], publishedAt: '2026-10-30', loadedAt: '2026-10-30',
    };
    const entities = [{ name: 'Синтетический работодатель', rank: 1,
      entityType: 'company', favorite: false, hidden: false }];
    window.fetch = async (input: RequestInfo | URL, options: RequestInit = {}) => {
      const url = String(input);
      if (url === '/api/session') return Response.json({ name: 'Тестовый администратор', isAdmin: true });
      if (url === '/api/tasks') return Response.json({ tasks: [], entities });
      if (url === '/api/google-calendar/status') return Response.json({ configured: false });
      if (url.startsWith('/api/vacancies/list?'))
        return Response.json({ entities, vacancies: [record], applicationSummary: [], roleTree: [] });
      if (url === '/api/channels') return Response.json({ channels: [] });
      if (url === '/api/vacancy-roles')
        return Response.json({ roles: [], roots: [], leaves: [], tree: [] });
      if (url === '/api/vacancies/71' && options.method === 'PUT') {
        const payload = JSON.parse(String(options.body));
        (window as any).__writes.push(payload);
        record = { ...record, ...payload, revision: 'b'.repeat(64) };
        return Response.json({ vacancies: [record] });
      }
      if (url === '/api/vacancies/71') return Response.json({ vacancy: record, entities });
      throw Error('Неожиданный синтетический запрос: ' + url);
    };
  });
  await page.addScriptTag({ content: source });
  const section = page.locator('.app');
  const titleButton = page.getByRole('button', {name: 'Синтетическая вакансия', exact: true});
  const editor = page.getByRole('dialog', {name: 'Редактирование вакансии'});
  const titleInput = () => editor.locator('input[maxlength="300"]').first();
  const enterVacancies = async () => {
    await page.locator('.entity-nav[data-section-menu="vacancies"] > .settings-nav-trigger').click();
    await expect(section).toHaveAttribute('data-section', 'vacancies');
  };
  const openEditor = async () => {
    await titleButton.click();
    const detail = page.getByRole('dialog', {name: 'Синтетическая вакансия'});
    await detail.getByRole('button', {name: 'Редактировать'}).click();
    await expect(editor).toBeVisible();
  };
  const nextConfirm = async (accepted: boolean) => page.evaluate(allow => {
    (window as any).__confirmAnswers.push(allow);
  }, accepted);
  const confirmationCount = () => page.evaluate(() => (window as any).__confirmMessages.length);
  const writeCount = () => page.evaluate(() => (window as any).__writes.length as number);
  return {section, editor, titleInput, enterVacancies, openEditor, nextConfirm, confirmationCount, writeCount};
}

type DirtyWorkspace = Awaited<ReturnType<typeof mountDirtyWorkspace>>;
const test = browserTest.extend<{workspace: DirtyWorkspace}>({
  workspace: async ({page}, use) => {
    const workspace = await mountDirtyWorkspace(page);
    await use(workspace);
    // Playwright closes the isolated Page and context. Browser exceptions
    // are checked by component-fixtures.ts without leaking their messages.
  },
});

test('26: Back отменяется при dirty-форме, подтверждённый Back/Forward восстанавливает карточку',
  async ({page, workspace}) => {
    const {editor, titleInput, enterVacancies, openEditor, nextConfirm, confirmationCount, writeCount} = workspace;
    await enterVacancies();
    await openEditor();
    await titleInput().fill('Мой несохранённый заголовок');
    await test.step('Отмена Back не закрывает редактор и не теряет введённый текст', async () => {
      await nextConfirm(false);
      await page.goBack();
      await expect(editor).toBeVisible();
      await expect(titleInput()).toHaveValue('Мой несохранённый заголовок');
      expect(await confirmationCount()).toBe(1);
    });
    await test.step('Подтверждение Back закрывает редактор, Forward возвращает карточку', async () => {
      await nextConfirm(true);
      await page.goBack();
      await expect(editor).toHaveCount(0);
      await page.goForward();
      const detail = page.getByRole('dialog', {name: 'Синтетическая вакансия', exact: true});
      await expect(detail).toBeVisible();
      await expect(detail.getByRole('button', {name: 'Редактировать'})).toBeVisible();
      expect(await confirmationCount()).toBe(2);
      expect(await writeCount()).toBe(0);
    });
  });

test('26–27: отказ сохраняет dirty-форму, подтверждение разрешает сменить раздел',
  async ({page, workspace}) => {
    const {section, editor, titleInput, enterVacancies, openEditor, nextConfirm, confirmationCount, writeCount} = workspace;
    await enterVacancies();
    await openEditor();
    await titleInput().fill('Мой несохранённый заголовок');
    await test.step('Отказ отменяет переход по кнопке Задачи', async () => {
      await nextConfirm(false);
      await page.getByRole('button', {name: 'Задачи', exact: true}).click();
      await expect(section).toHaveAttribute('data-section', 'vacancies');
      await expect(titleInput()).toHaveValue('Мой несохранённый заголовок');
      expect(await confirmationCount()).toBe(1);
    });
    await test.step('Подтверждение меняет раздел и размонтирует редактор', async () => {
      await nextConfirm(true);
      await page.getByRole('button', {name: 'Задачи', exact: true}).click();
      await expect(section).toHaveAttribute('data-section', 'tasks');
      await expect(editor).toHaveCount(0);
      expect(await confirmationCount()).toBe(2);
      expect(await writeCount()).toBe(0);
    });
  });

test('28: закрытие dirty-редактора требует подтверждения и сохраняет данные при отказе',
  async ({workspace}) => {
    const {editor, titleInput, enterVacancies, openEditor, nextConfirm, confirmationCount, writeCount} = workspace;
    await enterVacancies();
    await openEditor();
    await titleInput().fill('Правка при закрытии');
    await test.step('Отказ оставить изменения не закрывает редактор', async () => {
      await nextConfirm(false);
      await editor.getByRole('button', {name: 'Закрыть'}).click();
      await expect(editor).toBeVisible();
      await expect(titleInput()).toHaveValue('Правка при закрытии');
      expect(await confirmationCount()).toBe(1);
    });
    await test.step('Подтверждённое закрытие удаляет редактор без записи', async () => {
      await nextConfirm(true);
      await editor.getByRole('button', {name: 'Закрыть'}).click();
      await expect(editor).toHaveCount(0);
      expect(await confirmationCount()).toBe(2);
      expect(await writeCount()).toBe(0);
    });
  });

test('29–30: успешное сохранение снимает dirty-блокировку и не порождает второй PUT',
  async ({page, workspace}) => {
    const {section, editor, titleInput, enterVacancies, openEditor, confirmationCount, writeCount} = workspace;
    await enterVacancies();
    await openEditor();
    await titleInput().fill('Успешно сохранённый заголовок');
    await test.step('Сохранение вызывает один синтетический PUT и закрывает редактор', async () => {
      await editor.getByRole('button', {name: 'Сохранить изменения'}).click();
      await expect(editor).toHaveCount(0);
      expect(await writeCount()).toBe(1);
    });
    await test.step('После сохранения можно перейти без повторного confirm и PUT', async () => {
      const before = await confirmationCount();
      await page.getByRole('button', {name: 'Задачи', exact: true}).click();
      await expect(section).toHaveAttribute('data-section', 'tasks');
      expect(await confirmationCount()).toBe(before);
      expect(await writeCount()).toBe(1);
    });
  });
