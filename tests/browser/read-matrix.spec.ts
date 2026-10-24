import { test, expect } from './fixtures.js';

for (const account of [
  { login: 'eugene', name: 'Eugene', admin: true, records: 1 },
  { login: 'nastya', name: 'Nastya', admin: false, records: 0 },
  { login: 'demo', name: 'Demo', admin: false, records: 0 },
]) {
  test(`${account.name}: изоляция данных и чтение карточек`, async ({ page, activate }) => {
    const password = process.env.ITMAP_SMOKE_PASSWORD;
    if (!password) throw new Error('Explicit synthetic CI password is required.');
    await page.goto('/');
    await page.getByLabel('Логин', { exact: true }).fill(account.login);
    await page.getByLabel('Пароль', { exact: true }).fill(password);
    await activate(page.getByRole('button', { name: 'Открыть карту', exact: true }));
    await expect(page.getByRole('navigation', { name: 'Основные разделы' })).toBeVisible();
    const session = await page.request.get('/api/session');
    expect(session.status()).toBe(200);
    expect(await session.json()).toEqual({ name: account.name, role: account.admin ? 'admin' : 'user', isAdmin: account.admin });
    const tasks = page.locator('section[aria-label="Задачи"]');
    if (account.records) await expect(tasks.getByText('CI synthetic note', { exact: true })).toBeVisible();
    else {
      await expect(tasks.getByText('Задач пока нет.', { exact: true })).toBeVisible();
      await expect(tasks.getByText('CI synthetic note', { exact: true })).toHaveCount(0);
    }
    await activate(page.getByRole('button', { name: 'Отклики', exact: true }));
    const crm = await page.request.get('/api/crm/applications');
    expect(crm.status()).toBe(200);
    expect((await crm.json()).records).toHaveLength(account.records);
    if (account.records) {
      await expect(page.locator('.applications-table tbody tr')).toHaveCount(1);
      await expect(page.locator('.applications-table')).toContainText('CI Synthetic Vacancy');
    } else {
      await expect(page.locator('.applications-empty')).toBeVisible();
      await expect(page.locator('.applications-table tbody tr')).toHaveCount(0);
    }

    await activate(page.getByRole('button', { name: 'Список', exact: true }));
    await activate(page.locator('.company-list-name', { hasText: 'CI Synthetic Employer' }));
    const company = page.getByRole('dialog', { name: 'CI Synthetic Employer', exact: true });
    await expect(company.getByRole('heading', { name: 'Работодатель', exact: true })).toBeVisible();
    await activate(company.getByRole('button', { name: 'Закрыть карточку', exact: true }));
    await expect(company).toHaveCount(0);

    await activate(page.getByRole('button', { name: 'Вакансии', exact: true }));
    await expect(page.locator('.app')).toHaveAttribute('data-section', 'vacancies');
    await activate(page.getByRole('button', { name: 'CI Synthetic Vacancy', exact: true }));
    const vacancy = page.getByRole('dialog', { name: 'CI Synthetic Vacancy', exact: true });
    await expect(vacancy.getByText('Полный текст пока не добавлен.', { exact: true })).toBeVisible();
    await expect(vacancy.getByRole('button', { name: 'Редактировать', exact: true })).toHaveCount(account.admin ? 1 : 0);
    if (!account.admin) {
      // The real server must reject the role before any INSERT or SQL write.
      const denied = await page.request.post('/api/vacancies', { data: {} });
      expect(denied.status()).toBe(403);
    }
    await activate(vacancy.getByRole('tab', { name: 'Инфо', exact: true }));
    await expect(vacancy.getByRole('heading', { name: 'Краткая информация', exact: true })).toBeVisible();
    await activate(vacancy.getByRole('tab', { name: 'Задачи', exact: true }));
    if (account.records) await expect(vacancy.getByText('CI synthetic note', { exact: true })).toBeVisible();
    else await expect(vacancy.getByText('Задач пока нет.', { exact: true })).toBeVisible();
    await activate(vacancy.getByRole('button', { name: 'Закрыть', exact: true }));
    await expect(vacancy).toHaveCount(0);
    await page.goto('/logout');
    await expect(page.getByRole('button', { name: 'Открыть карту', exact: true })).toBeVisible();
  });
}
