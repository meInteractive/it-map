import { test, expect } from '../browser/fixtures.js';

// Только синтетический writable Go/MariaDB, без подмены успешных запросов.
test('Избранное, скрытие, перечитывание и изоляция пользователей', async ({ page, activate }) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password || process.env.ITMAP_COMPANY_BROWSER_WRITES !== '1') throw new Error('Требуется изолированный браузерный CI');
  const login = async (name: string) => {
    await page.goto('/');
    await page.getByLabel('Логин', { exact: true }).fill(name);
    await page.getByLabel('Пароль', { exact: true }).fill(password);
    await activate(page.getByRole('button', { name: 'Открыть карту', exact: true }));
    await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
  };
  const open = async () => {
    await activate(page.getByRole('button', { name: 'Список', exact: true }));
    await activate(page.locator('.company-list-name', { hasText: 'CI Synthetic Employer' }));
    return page.getByRole('dialog', { name: 'CI Synthetic Employer', exact: true });
  };
  const saved = async (path: string, key: 'favorite' | 'hidden', value: boolean, dialog: ReturnType<typeof page.getByRole>) => {
    const response = page.waitForResponse(r => {
      const u = new URL(r.url());
      return u.pathname === path && u.searchParams.get('company') === 'CI Synthetic Employer' && r.request().method() === 'PUT';
    });
    await activate(dialog.getByRole('button', { name: key === 'favorite' ? 'Избранное' : 'Скрыть компанию' }));
    expect((await response).status()).toBe(200);
    await expect(dialog.getByRole('button', { name: key === 'favorite' ? 'Избранное' : 'Скрыть компанию' })).toHaveAttribute('aria-pressed', String(value));
  };
  await login('demo');
  let dialog = await open();
  const favorite = dialog.getByRole('button', { name: 'Избранное' });
  await expect(favorite).toHaveAttribute('aria-pressed', 'false');

  // Управляемый отказ не является успешной записью. После возврата сети работает обычная MariaDB.
  await page.route('**/api/crm/favorite?*', async route => {
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"Синтетический отказ записи"}' });
  });
  await activate(favorite);
  await expect(dialog.getByRole('alert')).toContainText('Синтетический отказ записи');
  await expect(favorite).toHaveAttribute('aria-pressed', 'false');
  await page.unroute('**/api/crm/favorite?*');

  await saved('/api/crm/favorite', 'favorite', true, dialog);
  const favorites = await page.request.get('/api/crm/favorites');
  expect(favorites.status()).toBe(200);
  expect((await favorites.json()).companyNames).toContain('CI Synthetic Employer');
  await page.reload();
  dialog = await open();
  await expect(dialog.getByRole('button', { name: 'Избранное' })).toHaveAttribute('aria-pressed', 'true');
  await saved('/api/crm/favorite', 'favorite', false, dialog);

  await saved('/api/crm/hidden-company', 'hidden', true, dialog);
  const hidden = await page.request.get('/api/crm/hidden-companies');
  expect((await hidden.json()).companyNames).toContain('CI Synthetic Employer');
  await expect(dialog.getByRole('button', { name: 'Избранное' })).toHaveAttribute('aria-pressed', 'false');
  await page.reload();
  await activate(page.getByRole('button', { name: 'Список', exact: true }));
  await expect(page.locator('.company-list-name', { hasText: 'CI Synthetic Employer' })).toHaveCount(0);
  await activate(page.locator('.rank-filter-button'));
  await activate(page.getByText('Показать скрытые', { exact: true }));
  dialog = await open();
  await expect(dialog.getByRole('button', { name: 'Скрыть компанию' })).toHaveAttribute('aria-pressed', 'true');
  await saved('/api/crm/hidden-company', 'hidden', false, dialog);
  await page.goto('/logout');
  await login('nastya');
  const foreignFavorites = await page.request.get('/api/crm/favorites');
  const foreignHidden = await page.request.get('/api/crm/hidden-companies');
  expect((await foreignFavorites.json()).companyNames).not.toContain('CI Synthetic Employer');
  expect((await foreignHidden.json()).companyNames).not.toContain('CI Synthetic Employer');
});
