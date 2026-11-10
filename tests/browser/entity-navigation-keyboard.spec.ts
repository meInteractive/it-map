import {test, expect} from './component-fixtures.js';
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

test.beforeEach(async ({mountComponent}) => {
  await mountComponent(source);
});

test('1: ArrowDown открывает меню и переводит фокус на первый пункт', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const options = page.locator('#list-navigation .settings-nav-item');
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(options.first()).toBeFocused();
});

test('3: стрелки зацикливают перемещение по пунктам меню', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const options = page.locator('#list-navigation .settings-nav-item');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(1)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(2)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(options.last()).toBeFocused();
});

test('2: Home и End переводят фокус между крайними пунктами', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const options = page.locator('#list-navigation .settings-nav-item');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('End');
  await expect(options.last()).toBeFocused();
  await page.keyboard.press('Home');
  await expect(options.first()).toBeFocused();
});

test('4a: Escape закрывает меню и возвращает фокус на кнопку', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const menu = page.locator('#list-navigation');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(menu).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect(trigger).toBeFocused();
});

test('4b: ArrowUp открывает меню с фокусом на последнем пункте', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const options = page.locator('#list-navigation .settings-nav-item');
  await trigger.focus();
  await page.keyboard.press('ArrowUp');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(options.last()).toBeFocused();
});

test('5: отклонённый выбор не меняет раздел, повторное подтверждение срабатывает', async ({page}) => {
  const trigger = page.getByRole('button', {name: 'Список'});
  const menu = page.locator('#list-navigation');
  const options = menu.locator('.settings-nav-item');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(options.first()).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(options.nth(1)).toBeFocused();
  await page.evaluate(() => { (window as any).__allowChoice = false; });
  await page.keyboard.press('Enter');
  await expect(menu).toBeVisible();
  await expect(options.nth(1)).toBeFocused();
  await expect(options.first()).toHaveAttribute('aria-current', 'page');
  await expect(options.nth(1)).not.toHaveAttribute('aria-current', 'page');

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
