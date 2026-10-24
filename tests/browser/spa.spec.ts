import { test as base, expect } from '@playwright/test';

// These are real browser requests to the Go/MariaDB service, never mocked APIs.
// Restrict all traffic to this loopback origin; providers and map tiles are not
// part of this smoke. Do not persist cookies, passwords, traces or screenshots.
const test = base.extend<{ browserErrors: string[] }>({
  browserErrors: [async ({ page, context, baseURL }, use) => {
    const errors: string[] = [];
    page.on('pageerror', () => errors.push('Uncaught browser exception'));
    await context.route('**/*', route => {
      const url = new URL(route.request().url());
      if (url.origin !== new URL(baseURL!).origin) {
        errors.push('Unexpected external request');
        return route.abort();
      }
      return route.continue();
    });
    await use(errors);
    expect(errors).toEqual([]);
  }, { auto: true }],
});

test('страница входа: React, SPA fallback, стили и manifest', async ({ page }) => {
  const response = await page.goto('/browser-smoke/deep-link');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('button', { name: 'Открыть карту', exact: true })).toBeVisible();
  await expect(page.getByLabel('Логин', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Пароль', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => getComputedStyle(document.querySelector('.login-page')!).display)).toBe('grid');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await page.request.get('/api/session')).status()).toBe(401);
  expect((await page.request.get('/assets/missing-browser-smoke.js')).status()).toBe(404);
  const manifest = await page.request.get('/manifest.webmanifest');
  expect(manifest.status()).toBe(200);
  const data = await manifest.json();
  expect(data.start_url).toBe('/');
  for (const icon of ['/icons/icon-192.png', '/icons/icon-512.png']) {
    expect(data.icons.some((entry: { src: string }) => entry.src === icon)).toBe(true);
    expect((await page.request.get(icon)).status()).toBe(200);
  }
});

test('вход → задачи → список компаний → вакансии → выход', async ({ page, context }) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Explicit synthetic CI password is required.');
  await page.goto('/');
  await page.getByLabel('Логин', { exact: true }).fill('eugene');
  await page.getByLabel('Пароль', { exact: true }).fill(password);
  const login = page.waitForResponse(response => new URL(response.url()).pathname === '/auth');
  await page.getByRole('button', { name: 'Открыть карту', exact: true }).click();
  expect((await login).status()).toBe(303);
  await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
  const session = await page.request.get('/api/session');
  expect(session.status()).toBe(200);
  expect((await session.json()).name).toBe('Eugene');
  const cookie = (await context.cookies()).find(item => item.name === 'it_map_session');
  expect(cookie).toMatchObject({ httpOnly: true, secure: true, sameSite: 'Strict' });
  const tasks = page.locator('section[aria-label="Задачи"]');
  await expect(tasks.getByText('CI synthetic note', { exact: true })).toBeVisible();
  await expect(tasks.getByRole('button', { name: 'CI Synthetic Vacancy', exact: true })).toBeVisible();
  // Calendar integration remains explicitly unavailable in the migration shell;
  // these checks use the real independent task/catalog/vacancy read endpoints.
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await expect(page.locator('.company-list-name', { hasText: 'CI Synthetic Employer' })).toBeVisible();
  await page.getByRole('button', { name: 'Вакансии', exact: true }).click();
  await expect(page.getByRole('button', { name: 'CI Synthetic Vacancy', exact: true })).toBeVisible();
  await page.goto('/logout');
  await expect(page.getByRole('button', { name: 'Открыть карту', exact: true })).toBeVisible();
  expect((await page.request.get('/api/session')).status()).toBe(401);
  expect((await context.cookies()).some(item => item.name === 'it_map_session')).toBe(false);
});
