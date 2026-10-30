import { test, expect } from '@playwright/test';
import { build } from 'esbuild';

// Компилируем настоящий компонент в изолированный браузерный DOM без API.
const fixture = await build({
  stdin: {
    contents: [
      "import React, { useState } from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import { RankFilter, defaultFilters } from './frontend/features/companies/Filters';",
      "function Demo() {",
      "  const [filters, setFilters] = useState(defaultFilters);",
      "  return <><button id='outside'>Вне фильтра</button>",
      "    <RankFilter count={42} value={filters} onChange={next => {",
      "      window.__filterChanges.push({ ...next }); setFilters(next);",
      "    }} /></>;",
      "}",
      "window.__filterChanges = [];",
      "const root = createRoot(document.getElementById('root'));",
      "root.render(<Demo />);",
      "window.__unmountFilters = () => root.unmount();",
    ].join('\n'),
    sourcefile: 'filter-fixture.tsx',
    resolveDir: process.cwd(),
    loader: 'tsx',
  },
  bundle: true, write: false, platform: 'browser', format: 'iife', jsx: 'automatic',
});
const source = fixture.outputFiles[0].text;

test('16–20: клавиатура, фильтры и отписки настоящего FilterMenu', async ({ page }) => {
  await page.setContent('<main id="root"></main>');
  await page.evaluate(() => {
    const tracked = { pointerdown: new Set<EventListenerOrEventListenerObject>(),
      focusin: new Set<EventListenerOrEventListenerObject>() };
    const add = document.addEventListener.bind(document);
    const remove = document.removeEventListener.bind(document);
    document.addEventListener = ((type: string, listener: EventListenerOrEventListenerObject, options?: boolean | AddEventListenerOptions) => {
      if (options === true && type in tracked)
        tracked[type as keyof typeof tracked].add(listener);
      return add(type, listener, options);
    }) as typeof document.addEventListener;
    document.removeEventListener = ((type: string, listener: EventListenerOrEventListenerObject, options?: boolean | EventListenerOptions) => {
      if (options === true && type in tracked)
        tracked[type as keyof typeof tracked].delete(listener);
      return remove(type, listener, options);
    }) as typeof document.removeEventListener;
    (window as any).__filterListenerSizes = () =>
      Object.fromEntries(Object.entries(tracked).map(([key, set]) => [key, set.size]));
  });
  await page.addScriptTag({ content: source });
  const trigger = page.getByRole('button', { name: /Места/ });
  const menu = page.locator('.rank-filter-menu');
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect(menu).toBeHidden();

  // 16. Кнопка открывает и закрывает реальное меню.
  await trigger.click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(menu).toBeVisible();
  await expect.poll(() => page.evaluate(() => (window as any).__filterListenerSizes()))
    .toEqual({ pointerdown: 1, focusin: 1 });

  // 19. Изменение нескольких флажков сохраняет предыдущие значения.
  await page.getByRole('checkbox', { name: 'Избранные' }).check();
  await page.getByRole('checkbox', { name: 'Показать скрытые' }).check();
  await page.getByRole('checkbox', { name: 'С вакансиями' }).check();
  await expect(menu).toBeVisible();
  const changes = await page.evaluate(() => (window as any).__filterChanges as Record<string, boolean>[]);
  expect(changes).toHaveLength(3);
  expect(changes[0]).toMatchObject({ favoriteOnly: true, showHidden: false, vacanciesOnly: false });
  expect(changes[1]).toMatchObject({ favoriteOnly: true, showHidden: true, vacanciesOnly: false });
  expect(changes[2]).toMatchObject({ favoriteOnly: true, showHidden: true, vacanciesOnly: true });
  expect(changes[2].rankTop).toBe(true);

  // 17. Escape закрывает; повторное открытие сохраняет выбор.
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect.poll(() => page.evaluate(() => (window as any).__filterListenerSizes()))
    .toEqual({ pointerdown: 0, focusin: 0 });
  await trigger.click();
  await expect(page.getByRole('checkbox', { name: 'Избранные' })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Показать скрытые' })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'С вакансиями' })).toBeChecked();

  // 18. Действие внутри не закрывает меню; снаружи закрывает.
  await page.getByRole('checkbox', { name: 'С вакансиями' }).uncheck();
  await expect(menu).toBeVisible();
  await page.getByRole('button', { name: 'Вне фильтра' }).click();
  await expect(menu).toBeHidden();

  // 20. Повторное открытие не дублирует обработчики; unmount очищает.
  await trigger.click();
  await expect.poll(() => page.evaluate(() => (window as any).__filterListenerSizes()))
    .toEqual({ pointerdown: 1, focusin: 1 });
  await page.evaluate(() => (window as any).__unmountFilters());
  await expect.poll(() => page.evaluate(() => (window as any).__filterListenerSizes()))
    .toEqual({ pointerdown: 0, focusin: 0 });
});
