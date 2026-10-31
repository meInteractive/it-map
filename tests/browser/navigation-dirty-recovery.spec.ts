import { test, expect } from '@playwright/test';
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

test('26–30: настоящие переходы с изменённой формой и подтверждением', async ({ page }) => {
  await page.setContent('<main id="root"></main>');
  await page.route('**/*', route => route.abort('blockedbyclient'));
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
  const titleButton = page.getByRole('button', { name: 'Синтетическая вакансия', exact: true });
  const editor = page.getByRole('dialog', { name: 'Редактирование вакансии' });
  const titleInput = () => editor.locator('input[maxlength="300"]').first();
  const enterVacancies = async () => {
    await page.getByRole('button', { name: 'Вакансии', exact: true }).click();
    await expect(section).toHaveAttribute('data-section', 'vacancies');
  };
  const openEditor = async () => {
    await titleButton.click();
    const detail = page.getByRole('dialog', { name: 'Синтетическая вакансия' });
    await detail.getByRole('button', { name: 'Редактировать' }).click();
    await expect(editor).toBeVisible();
  };
  const nextConfirm = async (accepted: boolean) => page.evaluate(allow => {
    (window as any).__confirmAnswers.push(allow);
  }, accepted);
  const confirmationCount = () => page.evaluate(() => (window as any).__confirmMessages.length);

  await enterVacancies();
  await openEditor();
  await titleInput().fill('Мой несохранённый заголовок');
  // 26. Отказ блокирует реальный переход по кнопке раздела.
  await nextConfirm(false);
  await page.getByRole('button', { name: 'Задачи', exact: true }).click();
  await expect(section).toHaveAttribute('data-section', 'vacancies');
  await expect(titleInput()).toHaveValue('Мой несохранённый заголовок');
  expect(await confirmationCount()).toBe(1);

  // 27. Подтверждение разрешает переход, форма размонтируется.
  await nextConfirm(true);
  await page.getByRole('button', { name: 'Задачи', exact: true }).click();
  await expect(section).toHaveAttribute('data-section', 'tasks');
  await expect(editor).toHaveCount(0);
  expect(await confirmationCount()).toBe(2);

  // 28. Закрытие самой панели также требует подтверждения.
  await enterVacancies();
  await openEditor();
  await titleInput().fill('Правка при закрытии');
  await nextConfirm(false);
  await editor.getByRole('button', { name: 'Закрыть' }).click();
  await expect(editor).toBeVisible();
  await expect(titleInput()).toHaveValue('Правка при закрытии');
  await nextConfirm(true);
  await editor.getByRole('button', { name: 'Закрыть' }).click();
  await expect(editor).toHaveCount(0);

  // 29. После успешного сохранения dirty-подписка исчезает.
  await openEditor();
  await titleInput().fill('Успешно сохранённый заголовок');
  await editor.getByRole('button', { name: 'Сохранить изменения' }).click();
  await expect(editor).toHaveCount(0);
  const before = await confirmationCount();
  await page.getByRole('button', { name: 'Задачи', exact: true }).click();
  await expect(section).toHaveAttribute('data-section', 'tasks');
  expect(await confirmationCount()).toBe(before);
  expect(await page.evaluate(() => (window as any).__writes)).toHaveLength(1);

  // 30. Workspace хранит одну переменную editor и одну активную секцию.
  // Параллельное состояние двух отдельных редакторов штатной навигацией
  // не создаётся; два независимых useDirty уже проверяет test-dirty-navigation.mjs.
  await page.evaluate(() => (window as any).__unmountApplication());
});
