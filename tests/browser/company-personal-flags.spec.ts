import { test, expect, type Page } from '@playwright/test';
import { build } from 'esbuild';

// Реальный React-компонент и клиент; только синтетические HTTP-ответы в loopback CI.
const bundle = await build({
  stdin: {
    contents: [
      "import React from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import Application from './frontend/Application';",
      "createRoot(document.getElementById('root')).render(<Application />);",
    ].join('\n'),
    sourcefile: 'company-personal-flags-fixture.tsx',
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
const name = 'Синтетическая Альфа';

type Flags = { favorite: boolean; hidden: boolean };
type Write = { user: string; key: keyof Flags; value: boolean };
type Model = {
  user: 'Alpha' | 'Beta';
  users: Record<'Alpha' | 'Beta', Flags>;
  writes: Write[];
  listReads: number;
  sessionReads: string[];
  holdNextWrite?: Promise<void>;
  onWrite?: () => void;
  nextFailure?: 'lost_ack' | 'rejected';
  failReadOnce?: boolean;
};

const model = (): Model => ({
  user: 'Alpha',
  users: {
    Alpha: { favorite: false, hidden: false },
    Beta: { favorite: false, hidden: false },
  },
  writes: [],
  listReads: 0,
  sessionReads: [],
});

function entity(flags: Flags) {
  return {
    id: 1,
    name,
    rank: 1,
    entityType: 'company',
    favorite: flags.favorite,
    hidden: flags.hidden,
    offices: [],
    rating: null,
    salaryRangeSource: {},
    middle: [],
    senior: [],
    lead: [],
    head: [],
  };
}

async function fixture(page: Page, state: Model) {
  await page.route('**/*', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    const send = (data: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify(data),
      });
    if (req.resourceType() === 'document')
      return route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<!doctype html><html><body><main id="root"></main></body></html>',
      });
    if (path === '/api/session') {
      state.sessionReads.push(state.user);
      return send({ name: state.user, isAdmin: false });
    }
    if (path === '/api/tasks') return send({ tasks: [], entities: [] });
    if (path === '/api/google-calendar/status')
      return send({ configured: false, connected: false });
    if (path === '/api/companies/list') {
      state.listReads++;
      return send({ companies: [entity(state.users[state.user])] });
    }
    if (path === '/api/companies/detail')
      return send({ company: entity(state.users[state.user]) });
    if (path === '/api/crm' && req.method() === 'GET') {
      if (state.failReadOnce) {
        state.failReadOnce = false;
        return send({ error: 'Синтетический сбой повторного чтения' }, 503);
      }
      return send({
        status: '',
        comment: '',
        ...state.users[state.user],
        history: [],
      });
    }
    if (
      req.method() === 'PUT' &&
      (path === '/api/crm/favorite' || path === '/api/crm/hidden-company')
    ) {
      const user = state.user;
      const key: keyof Flags = path.endsWith('favorite') ? 'favorite' : 'hidden';
      const payload = req.postDataJSON() as Record<string, unknown>;
      if (url.searchParams.get('company') !== name || typeof payload[key] !== 'boolean')
        return send({ error: 'Неверный синтетический контракт' }, 400);
      const value = payload[key] as boolean;
      state.writes.push({ user, key, value });
      state.onWrite?.();
      const hold = state.holdNextWrite;
      state.holdNextWrite = undefined;
      if (hold) await hold;
      const mode = state.nextFailure;
      state.nextFailure = undefined;
      if (mode !== 'rejected') state.users[user][key] = value;
      if (mode) return send({ error: 'Синтетический отказ записи' }, 503);
      return send({
        status: '',
        comment: '',
        ...state.users[user],
        history: [],
      });
    }
    if (path.startsWith('/api/'))
      return send({ error: 'Непредусмотренный запрос: ' + path }, 500);
    return route.abort('blockedbyclient');
  });
  await page.goto('/__ci_company_flags_fixture');
  await open(page);
}

async function open(page: Page) {
  await page.addScriptTag({ content: script });
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await page.getByRole('button', { name, exact: true }).click();
  const dialog = page.getByRole('dialog', { name, exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Избранное' })).toBeVisible();
  return dialog;
}

test('S11-B03-03/05/08: двойной клик не дублирует запись, подтверждение видно после открытия карточки', async ({
  page,
}) => {
  const state = model();
  await fixture(page, state);
  const dialog = page.getByRole('dialog', { name, exact: true });
  const favorite = dialog.getByRole('button', { name: 'Избранное' });
  let release!: () => void;
  state.holdNextWrite = new Promise<void>((resolve) => {
    release = resolve;
  });
  await favorite.click();
  await expect.poll(() => state.writes.length).toBe(1);
  await expect(favorite).toBeDisabled();
  await favorite.evaluate((button: HTMLButtonElement) => button.click());
  expect(state.writes).toHaveLength(1);
  expect(state.users.Alpha.favorite).toBe(false);
  release();
  await expect(favorite).toHaveAttribute('aria-pressed', 'true');
  await expect(favorite).toBeEnabled();
  await expect.poll(() => state.listReads).toBeGreaterThan(1);
  await dialog.getByRole('button', { name: 'Закрыть карточку' }).click();
  await page.getByRole('button', { name, exact: true }).click();
  await expect(
    page.getByRole('dialog', { name, exact: true }).getByRole('button', { name: 'Избранное' }),
  ).toHaveAttribute('aria-pressed', 'true');
  expect(state.writes).toEqual([{ user: 'Alpha', key: 'favorite', value: true }]);
});

test('S11-B03-06/07: потерянный ответ PUT разрешается GET, повтор использует подтверждённый флаг', async ({
  page,
}) => {
  const state = model();
  state.nextFailure = 'lost_ack';
  await fixture(page, state);
  const dialog = page.getByRole('dialog', { name, exact: true });
  const favorite = dialog.getByRole('button', { name: 'Избранное' });
  await favorite.click();
  await expect(favorite).toHaveAttribute('aria-pressed', 'true');
  await expect(favorite).toBeEnabled();
  expect(state.users.Alpha.favorite).toBe(true);
  await favorite.click();
  await expect(favorite).toHaveAttribute('aria-pressed', 'false');
  expect(state.writes.map(({ value }) => value)).toEqual([true, false]);
  expect(state.users.Alpha.favorite).toBe(false);
});

test('S11-B03-07: если GET после неизвестного PUT недоступен, повторная запись заблокирована до чтения', async ({
  page,
}) => {
  const state = model();
  state.nextFailure = 'lost_ack';
  state.failReadOnce = true;
  await fixture(page, state);
  const dialog = page.getByRole('dialog', { name, exact: true });
  const favorite = dialog.getByRole('button', { name: 'Избранное' });
  await favorite.click();
  await expect(favorite).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Проверить состояние признаков' })).toBeVisible();
  expect(state.writes).toHaveLength(1);
  await dialog.getByRole('button', { name: 'Проверить состояние признаков' }).click();
  await expect(favorite).toBeEnabled();
  await expect(favorite).toHaveAttribute('aria-pressed', 'true');
  await favorite.click();
  await expect(favorite).toHaveAttribute('aria-pressed', 'false');
  expect(state.writes.map(({ value }) => value)).toEqual([true, false]);
});

test('S11-B03-01/02/04/10: после смены синтетического аккаунта личные признаки не переносятся', async ({
  page,
}) => {
  const state = model();
  await fixture(page, state);
  let dialog = page.getByRole('dialog', { name, exact: true });
  await dialog.getByRole('button', { name: 'Избранное' }).click();
  await expect(dialog.getByRole('button', { name: 'Избранное' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await dialog.getByRole('button', { name: 'Скрыть компанию' }).click();
  await expect(dialog.getByRole('button', { name: 'Скрыть компанию' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await dialog.getByRole('button', { name: 'Закрыть карточку' }).click();
  await page.getByRole('button', { name: 'Карта', exact: true }).click();
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
  state.user = 'Beta';
  // После logout новая вкладка должна начать с публичного адреса, а не
  // восстанавливать историю drawer прежнего пользователя.
  await page.goto('/__ci_company_flags_fixture');
  dialog = await open(page);
  await expect(dialog.getByRole('button', { name: 'Избранное' })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await expect(dialog.getByRole('button', { name: 'Скрыть компанию' })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  expect(state.users.Alpha).toEqual({ favorite: true, hidden: true });
  expect(state.users.Beta).toEqual({ favorite: false, hidden: false });
  expect(state.writes.every(({ user }) => user === 'Alpha')).toBe(true);
});

test('S11-B03-09/10: задержанный PUT старого пользователя не восстанавливает его флаг после нового /api/session', async ({
  page,
}) => {
  const state = model();
  await fixture(page, state);
  const dialog = page.getByRole('dialog', { name, exact: true });
  const favorite = dialog.getByRole('button', { name: 'Избранное' });
  let release!: () => void;
  state.holdNextWrite = new Promise<void>((resolve) => {
    release = resolve;
  });
  await favorite.click();
  await expect.poll(() => state.writes.length).toBe(1);
  await expect(favorite).toBeDisabled();
  // Новый успешный GET /api/session означает смену аккаунта в той же SPA.
  state.user = 'Beta';
  await page.evaluate(() => {
    const now = Date.now();
    Date.now = () => now + 120_000;
    window.dispatchEvent(new Event('focus'));
  });
  await expect.poll(() => state.sessionReads.includes('Beta')).toBe(true);
  // Дождаться commit нового Workspace, а не только получения /api/session.
  // На мобильном layout старый Workspace ещё может быть в фазе размонтирования.
  await expect(page.locator('.app')).toHaveAttribute('data-session-user', 'Beta');
  await expect(page.getByRole('dialog', { name, exact: true })).toHaveCount(0);
  // Поздний ответ прежнего PUT поступает уже после подтверждённого входа Beta.
  // Освобождаем его до повторного открытия карточки: на mobile перехватчик
  // Playwright может задерживать следующий GET detail, пока held route не завершён.
  // Проверка по-прежнему проходит через настоящий React и HTTP-клиент.
  release();
  await expect.poll(() => state.users.Alpha.favorite).toBe(true);
  await expect(page.locator('.app')).toHaveAttribute('data-session-user', 'Beta');
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await expect(page.getByRole('button', { name, exact: true })).toBeVisible();
  await page.getByRole('button', { name, exact: true }).click();
  const second = page.getByRole('dialog', { name, exact: true });
  try {
    await expect(second.getByRole('button', { name: 'Избранное' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  } catch (failure) {
    // Диагностика только синтетического fixture: отличаем отсутствие данных
    // от отсутствия панели/ошибки сессии, не скрывая неуспешный сценарий.
    console.log('S11-B03-09/10 mobile evidence:', JSON.stringify({
      identity: await page.locator('.app').getAttribute('data-session-user'),
      section: await page.locator('.app').getAttribute('data-section'),
      dialogCount: await second.count(),
      dialogText: (await second.count()) ? (await second.innerText()).slice(0, 600) : null,
      user: state.user,
      sessionReads: state.sessionReads,
      listReads: state.listReads,
      writes: state.writes,
    }));
    throw failure;
  }
  expect(state.users.Beta.favorite).toBe(false);
  expect(state.writes).toEqual([{ user: 'Alpha', key: 'favorite', value: true }]);
});
