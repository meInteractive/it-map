import {test, expect} from './fixtures.js';

// Только одноразовые синтетические аккаунты CI и локальный Go HTTP; внешние API не вызываются.
async function login(page: import('@playwright/test').Page, activate: (target: import('@playwright/test').Locator) => Promise<void>) {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Нужен синтетический пароль CI');
  await page.goto('/');
  await page.getByLabel('Логин', {exact: true}).fill('eugene');
  await page.getByLabel('Пароль', {exact: true}).fill(password);
  await activate(page.getByRole('button', {name: 'Открыть карту', exact: true}));
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
}

test('01, 10: истёкшая cookie на списке очищает строки, reload не показывает приватные данные', async ({page, context, activate}) => {
  await login(page, activate);
  await activate(page.getByRole('button', {name: 'Список', exact: true}));
  await expect(page.getByRole('button', {name: 'CI Synthetic Employer', exact: true})).toBeVisible();

  await context.clearCookies();
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('button', {name: 'Открыть карту', exact: true})).toBeVisible();
  await expect(page.getByText('CI Synthetic Employer')).toHaveCount(0);
  await expect(page.locator('.company-list-table')).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('button', {name: 'Открыть карту', exact: true})).toBeVisible();
  await expect(page.locator('.app')).toHaveCount(0);
});

test('02, 06: 401 при обновлении открытой вакансии удаляет приватную карточку и не повторяет write', async ({page, context, activate}) => {
  await login(page, activate);
  await activate(page.getByRole('button', {name: 'Вакансии', exact: true}));
  await activate(page.getByRole('button', {name: 'CI Synthetic Vacancy', exact: true}));
  await expect(page.getByRole('dialog', {name: 'CI Synthetic Vacancy', exact: true})).toBeVisible();

  const logoutRequests: string[] = [];
  const writes: string[] = [];
  page.on('request', request => {
    const path = new URL(request.url()).pathname;
    if (path === '/logout') logoutRequests.push(path);
    if (!['GET', 'HEAD'].includes(request.method())) writes.push(path);
  });
  await context.clearCookies();
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('button', {name: 'Открыть карту', exact: true})).toBeVisible();
  await expect(page.getByRole('dialog', {name: 'CI Synthetic Vacancy', exact: true})).toHaveCount(0);
  await expect(page.locator('.app')).toHaveCount(0);
  expect(logoutRequests).toHaveLength(1);
  expect(writes).toHaveLength(0);
});

test('04, 09: выход в соседней вкладке закрывает несохранённую приватную форму после следующего запроса', async ({page, context, activate}) => {
  await login(page, activate);
  const other = await context.newPage();
  try {
    await other.goto('/');
    await expect(other.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
    await other.getByRole('button', {name: 'Вакансии', exact: true}).click();
    await other.getByRole('button', {name: 'CI Synthetic Vacancy', exact: true}).click();
    await other.getByRole('dialog', {name: 'CI Synthetic Vacancy', exact: true})
      .getByRole('button', {name: 'Редактировать', exact: true}).click();
    const editor = other.getByRole('dialog', {name: 'Редактирование вакансии'});
    await expect(editor).toBeVisible();
    const title = editor.locator('input[maxlength="300"]').first();
    await title.fill('Несохранённый приватный черновик');
    await expect(title).toHaveValue('Несохранённый приватный черновик');

    await page.goto('/logout');
    await expect(page.getByRole('button', {name: 'Открыть карту', exact: true})).toBeVisible();
    await other.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect(other.getByRole('button', {name: 'Открыть карту', exact: true})).toBeVisible();
    await expect(other.getByText('Несохранённый приватный черновик')).toHaveCount(0);
    await expect(editor).toHaveCount(0);
  } finally {
    await other.close();
  }
});
