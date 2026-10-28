import { readFile } from 'node:fs/promises';
import type { Page, Locator } from '@playwright/test';
import { test, expect } from '../browser/fixtures.js';

async function openGeneral(page: Page, activate: (target: Locator) => Promise<void>) {
  await activate(page.getByRole('button', { name: 'Настройки', exact: true }));
  await activate(page.getByRole('button', { name: 'Общие', exact: true }));
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'settings');
  await expect(page.getByRole('heading', { name: 'Настройки · Общие', exact: true })).toBeVisible();
}

async function login(page: Page, activate: (target: Locator) => Promise<void>, account = 'eugene') {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password || process.env.ITMAP_BACKUP_BROWSER_WRITES !== '1') throw new Error('Guarded synthetic backup credentials required.');
  await page.goto('/');
  await page.getByLabel('Логин', { exact: true }).fill(account);
  await page.getByLabel('Пароль', { exact: true }).fill(password);
  await activate(page.getByRole('button', { name: 'Открыть карту', exact: true }));
  await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
  await expect(page.locator('.app')).toHaveAttribute('data-admin', String(account === 'eugene'));
  await openGeneral(page, activate);
}
const restoreCard = (page: Page) => page.locator('article').filter({ has: page.getByRole('heading', { name: 'Восстановить из бэкапа', exact: true }) });

test('Бэкап: скачать, проверить, отменить и восстановить сохранённые данные', async ({ page, activate }) => {
  await login(page, activate);
  const downloadEvent = page.waitForEvent('download');
  await activate(page.getByRole('button', { name: 'Скачать бэкап', exact: true }));
  const download = await downloadEvent, filePath = await download.path();
  if (!filePath) throw new Error('Synthetic download missing.');
  const body = await readFile(filePath);
  expect(JSON.parse(body.toString()).version).toBe(39);
  // Change real synthetic SQL state after the snapshot, then prove UI restore
  // replaces it. This is fixture setup through an already accepted API.
  const changed = await page.request.put('/api/crm/favorite?company=CI%20Synthetic%20Employer', { data: { favorite: true } });
  expect(changed.status()).toBe(200);
  expect((await (await page.request.get('/api/crm/favorites')).json()).companyNames).toEqual(['CI Synthetic Employer']);
  let posts = 0;
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/backup' && r.method() === 'POST') posts++; });
  const card = restoreCard(page), button = card.getByRole('button', { name: 'Восстановить данные', exact: true });
  await expect(button).toBeDisabled();
  const validation = page.waitForResponse(r => new URL(r.url()).pathname === '/api/backup/validate' && r.request().method() === 'POST');
  await card.locator('input[type=file]').setInputFiles({ name: 'CI-backup-v39.json', mimeType: 'application/json', buffer: body });
  expect((await validation).status()).toBe(200);
  await expect(card.locator('.backup-preview')).toContainText('Разделов данных:');
  await expect(button).toBeEnabled();
  expect(posts).toBe(0);
  page.once('dialog', dialog => dialog.dismiss());
  await activate(button);
  expect(posts).toBe(0);
  expect((await (await page.request.get('/api/crm/favorites')).json()).companyNames).toEqual(['CI Synthetic Employer']);
  const restored = page.waitForResponse(r => new URL(r.url()).pathname === '/api/backup' && r.request().method() === 'POST');
  const reloaded = page.waitForEvent('load');
  page.once('dialog', dialog => dialog.accept());
  await activate(button);
  expect((await restored).status()).toBe(200);
  await reloaded;
  await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
  await openGeneral(page, activate);
  await expect(restoreCard(page).getByRole('button', { name: 'Восстановить данные', exact: true })).toBeDisabled();
  expect(posts).toBe(1);
  const favorites = await page.request.get('/api/crm/favorites');
  expect(favorites.status()).toBe(200);
  expect((await favorites.json()).companyNames).toEqual([]);
});

test('Бэкап: невалидный и слишком большой файл не разрешают восстановление', async ({ page, activate }) => {
  await login(page, activate);
  const card = restoreCard(page), input = card.locator('input[type=file]'), button = card.getByRole('button', { name: 'Восстановить данные', exact: true });
  let posts = 0;
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/backup' && r.method() === 'POST') posts++; });
  await input.setInputFiles({ name: 'CI-malformed.json', mimeType: 'application/json', buffer: Buffer.from('{') });
  await expect(card.getByRole('alert')).toBeVisible();
  await expect(button).toBeDisabled();
  await input.setInputFiles({ name: 'CI-large.json', mimeType: 'application/json', buffer: Buffer.alloc(25 * 1024 * 1024 + 1, 32) });
  await expect(card).toContainText('Файл больше допустимых 25 МБ.');
  await expect(button).toBeDisabled();
  const validation = page.waitForResponse(r => new URL(r.url()).pathname === '/api/backup/validate' && r.request().method() === 'POST');
  await input.setInputFiles({ name: 'CI-invalid-shape.json', mimeType: 'application/json', buffer: Buffer.from('{}') });
  expect((await validation).status()).toBe(400);
  await expect(button).toBeDisabled();
  expect(posts).toBe(0);
});

test('Бэкап: пользовательские роли не получают восстановление и скачивание', async ({ page, activate }) => {
  for (const account of ['nastya', 'demo']) {
    await login(page, activate, account);
    await expect(page.getByRole('button', { name: 'Скачать бэкап', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Восстановить данные', exact: true })).toHaveCount(0);
    expect((await page.request.get('/api/backup')).status()).toBe(403);
    expect((await page.request.post('/api/backup/validate', { data: {} })).status()).toBe(403);
    expect((await page.request.post('/api/backup', { data: {} })).status()).toBe(403);
    await page.goto('/logout');
  }
});
