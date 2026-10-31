import {test, expect} from './fixtures.js';

test.use({serviceWorkers: 'allow'});

// Одноразовый тестовый сервис CI, без внешних провайдеров и продуктивных данных.
test('24, 25, 30: offline скрывает приватный список, online заново проверяет /api/session', async ({page, context, activate}) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Нужен синтетический пароль CI');
  await page.goto('/');
  await page.getByLabel('Логин', {exact: true}).fill('eugene');
  await page.getByLabel('Пароль', {exact: true}).fill(password);
  await activate(page.getByRole('button', {name: 'Открыть карту', exact: true}));
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
  await activate(page.getByRole('button', {name: 'Список', exact: true}));
  await expect(page.getByRole('button', {name: 'CI Synthetic Employer', exact: true})).toBeVisible();
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });

  await context.setOffline(true);
  try {
    await expect(page.getByRole('heading', {name: 'Нет подключения к сети'})).toBeVisible();
    await expect(page.getByText('Приватные данные недоступны без проверки сессии.', {exact: false})).toBeVisible();
    await expect(page.getByText('CI Synthetic Employer')).toHaveCount(0);
    await expect(page.locator('.app')).toHaveCount(0);
    const paths = await page.evaluate(async () => {
      const result: string[] = [];
      for (const key of await caches.keys()) {
        for (const req of await (await caches.open(key)).keys()) {
          result.push(new URL(req.url).pathname);
        }
      }
      return result.sort();
    });
    expect(paths).toEqual(['/icons/apple-touch-icon.png', '/icons/icon-192.png',
      '/icons/icon-512.png', '/manifest.webmanifest', '/offline.html'].sort());
  } finally {
    const verified = page.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/session' && response.status() === 200);
    await context.setOffline(false);
    await verified;
  }
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
  await expect(page.locator('.app')).toBeVisible();
  await page.goto('/logout');
});

test('21–23: manifest desktop/mobile использует один корневой start_url и существующие иконки', async ({page}) => {
  await page.goto('/');
  const response = await page.request.get('/manifest.webmanifest');
  expect(response.status()).toBe(200);
  const manifest = await response.json();
  expect(manifest.id).toBe('/');
  expect(manifest.start_url).toBe('/');
  expect(manifest.scope).toBe('/');
  expect(manifest.display).toBe('standalone');
  for (const icon of manifest.icons) {
    expect(icon.src).toMatch(/^\/icons\/icon-(192|512)\.png$/);
    expect((await page.request.get(icon.src)).status()).toBe(200);
  }
});

test('24, 26: холодный offline-запуск показывает только публичную страницу, после сети требуется новый вход/сессия', async ({page, context, activate}) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Нужен синтетический пароль CI');
  await page.goto('/');
  await page.getByLabel('Логин', {exact: true}).fill('eugene');
  await page.getByLabel('Пароль', {exact: true}).fill(password);
  await activate(page.getByRole('button', {name: 'Открыть карту', exact: true}));
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.waitForFunction(() => navigator.serviceWorker.controller?.state === 'activated');
  await context.setOffline(true);
  try {
    await page.reload();
    await expect(page.getByRole('heading', {name: 'Нет подключения к сети'})).toBeVisible();
    await expect(page.getByText('Эта страница не содержит сохранённых приватных данных.', {exact: false})).toBeVisible();
    await expect(page.locator('.app')).toHaveCount(0);
  } finally {
    await context.setOffline(false);
  }
  await page.reload();
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
  await page.goto('/logout');
});
