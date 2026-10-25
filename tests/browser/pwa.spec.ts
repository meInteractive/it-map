import { test, expect } from './fixtures.js';

test.use({ serviceWorkers: 'allow' });

test('PWA: установка worker, offline public assets, API не кэшируется', async ({ page, context, activate }) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Explicit synthetic CI password is required.');
  await page.goto('/');
  await page.getByLabel('Логин', { exact: true }).fill('eugene');
  await page.getByLabel('Пароль', { exact: true }).fill(password);
  await activate(page.getByRole('button', { name: 'Открыть карту', exact: true }));
  await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.waitForFunction(() => navigator.serviceWorker.controller?.state === 'activated');

  // These fetches pass through the real worker, unlike page.request/APIRequestContext.
  const online = await page.evaluate(async () => Promise.all(
    ['/api/session', '/api/tasks', '/'].map(async url => (await fetch(url)).status),
  ));
  expect(online).toEqual([200, 200, 200]);
  const publicPaths = ['/icons/apple-touch-icon.png', '/icons/icon-192.png', '/icons/icon-512.png', '/manifest.webmanifest'];
  const cachedPaths = await page.evaluate(async () => {
    const paths: string[] = [];
    for (const name of await caches.keys()) {
      for (const request of await (await caches.open(name)).keys()) {
        paths.push(new URL(request.url).pathname);
      }
    }
    return paths.sort();
  });
  // No HTML, session, task notes, auth responses, or arbitrary assets in caches.
  expect(cachedPaths).toEqual(publicPaths);
  await context.setOffline(true);
  try {
    const cached = await page.evaluate(async paths => Promise.all(paths.map(async url => {
      const response = await fetch(url);
      return { status: response.status, bytes: (await response.arrayBuffer()).byteLength };
    })), publicPaths);
    for (const response of cached) {
      expect(response.status).toBe(200);
      expect(response.bytes).toBeGreaterThan(0);
    }
    const privateReachable = await page.evaluate(async () => Promise.all(
      ['/api/session', '/api/tasks', '/'].map(async url => {
        try { await fetch(url); return true; }
        catch { return false; }
      }),
    ));
    expect(privateReachable).toEqual([false, false, false]);
  } finally {
    await context.setOffline(false);
  }
  await page.goto('/logout');
  await expect(page.getByRole('button', { name: 'Открыть карту', exact: true })).toBeVisible();
});
