import { test, expect } from '@playwright/test';
import { build } from 'esbuild';

// Настоящий компонент в изолированном Chromium; API и сервер не требуются.
const fixture = await build({
  stdin: {
    contents: [
      "import React, { useState } from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import { EntityNavigation } from './frontend/components/EntityNavigation';",
      "function Demo() {",
      "  const [type, setType] = useState('company');",
      "  return <EntityNavigation section='list' label='Список' icon='list' active type={type}",
      "    onSelect={next => {",
      "      window.__choices.push(next);",
      "      if (!window.__allowChoice) return false;",
      "      setType(next); return true;",
      "    }} />;",
      "}",
      "window.__choices = []; window.__allowChoice = true;",
      "const root = createRoot(document.getElementById('root'));",
      "root.render(<Demo />); window.__unmountEntityNavigation = () => root.unmount();",
    ].join('\n'),
    sourcefile: 'entity-navigation-fixture.tsx', resolveDir: process.cwd(), loader: 'tsx',
  },
  bundle: true, write: false, platform: 'browser', format: 'iife', jsx: 'automatic',
});
const source = fixture.outputFiles[0].text;

test('1–5: клавиатурная навигация организаций и подтверждение выбора', async ({ page }) => {
  await page.setContent('<main id="root"></main>');
  await page.addScriptTag({ content: source });
  const trigger = page.getByRole('button', { name: 'Список' });
  const menu = page.locator('#list-navigation');
  const options = menu.locator('.settings-nav-item');
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');

  // 1. ArrowDown открывает меню с фокусом на первом пункте.
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(options.first()).toBeFocused();
  // 3. Следующий элемент, зацикливание последнего на первый.
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(1)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(2)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(options.last()).toBeFocused();

  // 4. Home/End и Escape: фокус возвращается к переключателю.
  await page.keyboard.press('Home');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('End');
  await expect(options.last()).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect(trigger).toBeFocused();

  // 2. ArrowUp открывает с последнего пункта.
  await page.keyboard.press('ArrowUp');
  await expect(options.last()).toBeFocused();

  // 5. Отказ в onSelect сохраняет открытый список и текущий тип.
  await page.evaluate(() => { (window as any).__allowChoice = false; });
  await page.keyboard.press('Home');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect(options.nth(1)).toBeFocused();
  await expect(menu).toBeVisible();
  await expect(options.first()).toHaveAttribute('aria-current', 'page');
  await expect(options.nth(1)).not.toHaveAttribute('aria-current', 'page');

  // Успешный выбор меняет активный пункт и возвращает фокус.
  await page.evaluate(() => { (window as any).__allowChoice = true; });
  await page.keyboard.press('Enter');
  await expect(menu).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  expect(await page.evaluate(() => (window as any).__choices)).toEqual(['agency', 'agency']);
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(1)).toHaveAttribute('aria-current', 'page');
});
