import {test, expect} from './component-fixtures.js';
import { build } from 'esbuild';

// Монтируем настоящий Drawer в Chromium без HTTP/API и тестового сервера.
const fixture = await build({
  stdin: {
    contents: [
      "import React, { useState } from 'react';",
      "import { createRoot } from 'react-dom/client';",
      "import { Drawer } from './frontend/components/Drawer';",
      "function Demo() {",
      "  const [open, setOpen] = useState(false);",
      "  return <><button id='opener' onClick={() => setOpen(true)}>Показать карточку</button>",
      "    {open && <Drawer title='Тестовая карточка' onClose={() => {",
      "      window.__closeCount++; setOpen(false);",
      "    }}><button id='extra'>Действие</button>",
      "       <a id='last-link' href='#synthetic'>Последняя ссылка</a>",
      "       <button id='disabled-control' disabled>Недоступное действие</button>",
      "    </Drawer>}</>;",
      "}",
      "window.__closeCount = 0;",
      "const root = createRoot(document.getElementById('root'));",
      "root.render(<Demo />);",
      "window.__unmountDrawer = () => root.unmount();",
    ].join('\n'),
    sourcefile: 'drawer-fixture.tsx',
    resolveDir: process.cwd(),
    loader: 'tsx',
  },
  bundle: true, write: false, platform: 'browser', format: 'iife', jsx: 'automatic',
});
const source = fixture.outputFiles[0].text;

// Каждый тест получает новый Page и новую React-панель: порядок выполнения не важен.
test.beforeEach(async ({page, mountComponent}) => {
  await mountComponent(source, async () => {
    await page.evaluate(() => {
      const callbacks = new Set<EventListenerOrEventListenerObject>();
      const add = Element.prototype.addEventListener;
      const remove = Element.prototype.removeEventListener;
      Element.prototype.addEventListener = function (type: string, listener: EventListenerOrEventListenerObject, options?: boolean | AddEventListenerOptions) {
        if (this.tagName === 'ASIDE' && type === 'keydown') callbacks.add(listener);
        return add.call(this, type, listener, options);
      };
      Element.prototype.removeEventListener = function (type: string, listener: EventListenerOrEventListenerObject, options?: boolean | EventListenerOptions) {
        if (this.tagName === 'ASIDE' && type === 'keydown') callbacks.delete(listener);
        return remove.call(this, type, listener, options);
      };
      (window as any).__drawerListeners = () => callbacks.size;
    });
  });
});

test('26: Drawer имеет доступное имя, начальный фокус и ровно один обработчик', async ({page}) => {
  const opener = page.getByRole('button', {name: 'Показать карточку'});
  const dialog = page.getByRole('dialog', {name: 'Тестовая карточка'});
  const close = dialog.getByRole('button', {name: 'Закрыть'});
  await opener.click();
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute('aria-label', 'Тестовая карточка');
  await expect(close).toBeFocused();
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(1);
});

test('28–29: Tab зациклен по доступным элементам в обе стороны', async ({page}) => {
  await page.getByRole('button', {name: 'Показать карточку'}).click();
  const dialog = page.getByRole('dialog', {name: 'Тестовая карточка'});
  const close = dialog.getByRole('button', {name: 'Закрыть'});
  const last = dialog.getByRole('link', {name: 'Последняя ссылка'});
  await expect(close).toBeFocused();
  await expect(dialog.getByRole('button', {name: 'Недоступное действие'})).toBeDisabled();
  await last.focus();
  await page.keyboard.press('Tab');
  await expect(close).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(last).toBeFocused();
});

test('27, 30: Escape закрывает один раз и восстанавливает фокус без утечки', async ({page}) => {
  const opener = page.getByRole('button', {name: 'Показать карточку'});
  const dialog = page.getByRole('dialog', {name: 'Тестовая карточка'});
  await opener.click();
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  expect(await page.evaluate(() => (window as any).__closeCount)).toBe(1);
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(0);
  await page.keyboard.press('Escape');
  expect(await page.evaluate(() => (window as any).__closeCount)).toBe(1);
});

test('30: повторное открытие и закрытие кнопкой очищают каждый обработчик', async ({page}) => {
  const opener = page.getByRole('button', {name: 'Показать карточку'});
  const dialog = page.getByRole('dialog', {name: 'Тестовая карточка'});
  for (let closeCount = 1; closeCount <= 2; closeCount++) {
    await opener.click();
    const close = dialog.getByRole('button', {name: 'Закрыть'});
    await expect(close).toBeFocused();
    expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(1);
    await close.click();
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(await page.evaluate(() => (window as any).__closeCount)).toBe(closeCount);
    expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(0);
  }
  await page.evaluate(() => (window as any).__unmountDrawer());
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(0);
});
