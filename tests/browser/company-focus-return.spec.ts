import {test, expect} from '@playwright/test';
import {build} from 'esbuild';

// Изолированный Chromium: реальная таблица и карточка, только синтетические API.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import {createRoot} from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "const root = createRoot(document.getElementById('root'));",
      "root.render(<Application />);",
    ].join('\n'),
    sourcefile: 'company-focus-fixture.tsx', resolveDir: process.cwd(), loader: 'tsx',
  },
  bundle: true, write: false, platform: 'browser', format: 'iife', jsx: 'automatic',
  plugins: [{
    name: 'empty-test-css',
    setup(build) {
      build.onResolve({filter: /\.css$/}, args => ({path: args.path, namespace: 'test-css'}));
      build.onLoad({filter: /.*/, namespace: 'test-css'}, () => ({contents: '', loader: 'js'}));
    },
  }],
});
const source = bundle.outputFiles[0].text;

test('28: после закрытия карточки компании фокус возвращается к исходной строке', async ({page}) => {
  await page.setContent('<main id="root"></main>');
  await page.route('**/*', route => route.abort('blockedbyclient'));
  await page.evaluate(() => {
    const company = {
      id: 1, name: 'Синтетическая Альфа', rank: 1, entityType: 'company',
      favorite: false, hidden: false, offices: [], rating: null,
      salaryRangeSource: {}, middle: [], senior: [], lead: [], head: [],
    };
    window.fetch = async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === '/api/session') return Response.json({name: 'Синтетический пользователь', isAdmin: false});
      if (url === '/api/tasks') return Response.json({tasks: [], entities: []});
      if (url === '/api/google-calendar/status') return Response.json({configured: false});
      if (url === '/api/companies/list') return Response.json({companies: [company]});
      if (url.startsWith('/api/companies/detail?'))
        return Response.json({error: 'Синтетический отказ загрузки подробностей'}, {status: 503});
      throw Error('Неожиданный синтетический запрос: ' + url);
    };
  });
  await page.addScriptTag({content: source});
  await page.getByRole('button', {name: 'Список', exact: true}).click();
  const opener = page.getByRole('button', {name: 'Синтетическая Альфа', exact: true});
  await opener.focus();
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', {name: 'Синтетическая Альфа', exact: true});
  await expect(dialog).toBeVisible();
  const close = dialog.getByRole('button', {name: 'Закрыть карточку'});
  await expect(close).toBeFocused();
  await expect(dialog.getByRole('alert')).toHaveText('Синтетический отказ загрузки подробностей');
  await close.click();
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
});
