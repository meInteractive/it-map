import {test, expect} from './fixtures.js';

async function login(page: import('@playwright/test').Page, activate: (target: import('@playwright/test').Locator) => Promise<void>, path = '/') {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password) throw new Error('Нужен пароль одноразового синтетического аккаунта CI');
  await page.goto(path);
  await page.getByLabel('Логин', {exact: true}).fill('eugene');
  await page.getByLabel('Пароль', {exact: true}).fill(password);
  await activate(page.getByRole('button', {name: 'Открыть карту', exact: true}));
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
}

test('11: прямой URL вакансий переживает вход', async ({page, activate}) => {
  await login(page, activate, '/vacancies');
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'vacancies');
  await expect(page).toHaveURL(/\/vacancies$/);
  await expect(page.getByRole('button', {name: 'CI Synthetic Vacancy', exact: true})).toBeVisible();
  await page.goto('/logout');
});

test('12: неизвестный путь не создаёт пустой экран', async ({page, activate}) => {
  await login(page, activate, '/unknown-ci-route');
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'tasks');
  await expect(page.getByRole('navigation', {name: 'Основные разделы'})).toBeVisible();
  await page.goto('/logout');
});

test('13, 17, 20: Back/Forward сохраняют фильтр и видимый клавиатурный фокус', async ({page, activate}) => {
  await login(page, activate);
  const list = page.getByRole('button', {name: 'Список', exact: true});
  await list.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'list');
  await page.getByLabel('Поиск компаний').fill('CI Synthetic');
  await expect(page.getByRole('button', {name: 'CI Synthetic Employer', exact: true})).toBeVisible();
  await activate(page.getByRole('button', {name: 'Вакансии', exact: true}));
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'vacancies');
  await page.goBack();
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'list');
  await expect(page.getByLabel('Поиск компаний')).toHaveValue('CI Synthetic');
  await expect(list).toBeFocused();
  await page.goForward();
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'vacancies');
  await page.goto('/logout');
});

test('13–14: Back закрывает карточку компании, Forward возвращает ту же карточку без приватного URL', async ({page, activate}) => {
  await login(page, activate);
  await activate(page.getByRole('button', {name: 'Список', exact: true}));
  const opener = page.getByRole('button', {name: 'CI Synthetic Employer', exact: true});
  await opener.focus();
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', {name: 'CI Synthetic Employer', exact: true});
  await expect(dialog).toBeVisible();
  const drawerState = await page.evaluate(() => history.state);
  expect(Object.keys(drawerState).sort()).toEqual(['itmapDrawerToken', 'itmapPosition']);
  expect(Number.isSafeInteger(drawerState.itmapDrawerToken)).toBe(true);
  expect(Number.isSafeInteger(drawerState.itmapPosition)).toBe(true);
  await expect(page).toHaveURL(/\/list$/);
  await page.goBack();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.app')).toHaveAttribute('data-section', 'list');
  await page.goForward();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading', {name: 'Работодатель', exact: true})).toBeVisible();
  await activate(dialog.getByRole('button', {name: 'Закрыть карточку', exact: true}));
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  await page.goto('/logout');
});
