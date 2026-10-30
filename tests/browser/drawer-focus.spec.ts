import { test, expect } from '@playwright/test';
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

test('26–30: фокус, Escape, Tab и очистка обработчика Drawer', async ({ page }) => {
  await page.setContent('<main id="root"></main>');
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
  await page.addScriptTag({ content: source });

  const opener = page.getByRole('button', { name: 'Показать карточку' });
  const dialog = page.getByRole('dialog', { name: 'Тестовая карточка' });
  const close = page.getByRole('button', { name: 'Закрыть' });
  const last = page.getByRole('link', { name: 'Последняя ссылка' });

  // 26. Доступная роль и имя, начальный фокус на кнопке закрытия.
  await opener.click();
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute('aria-label', 'Тестовая карточка');
  await expect(close).toBeFocused();
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(1);

  // 28. Tab с последней доступной ссылки возвращает на первую кнопку.
  await last.focus();
  await page.keyboard.press('Tab');
  await expect(close).toBeFocused();

  // 29. Shift+Tab с первой кнопки возвращает на последнюю ссылку.
  await page.keyboard.press('Shift+Tab');
  await expect(last).toBeFocused();
  await expect(dialog.getByRole('button', { name: 'Недоступное действие' })).toBeDisabled();

  // 27 и 30. Escape вызывает закрытие однократно и возвращает прежний фокус.
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  expect(await page.evaluate(() => (window as any).__closeCount)).toBe(1);
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(0);
  await page.keyboard.press('Escape');
  expect(await page.evaluate(() => (window as any).__closeCount)).toBe(1);

  // Отдельное открытие и закрытие кнопкой; опять очистка обработчика.
  await opener.click();
  await expect(close).toBeFocused();
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(1);
  await close.click();
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  expect(await page.evaluate(() => (window as any).__closeCount)).toBe(2);
  expect(await page.evaluate(() => (window as any).__drawerListeners())).toBe(0);
  await page.evaluate(() => (window as any).__unmountDrawer());
});
