import {test as base, expect, type Page} from '@playwright/test';

type MountComponent = (bundle: string, beforeMount?: () => Promise<void>) => Promise<void>;

// Настоящие React-компоненты монтируются в новом Chromium Page каждого теста.
// Запрещаем внешние запросы, не сохраняем cookies, трассировки и HTTP payload.
export const test = base.extend<{
  mountComponent: MountComponent;
  browserErrorTypes: string[];
}>({
  browserErrorTypes: [async ({page}, use) => {
    const errors: string[] = [];
    // Только имя исключения: message/stack может содержать секреты и текст API.
    page.on('pageerror', error => errors.push(error.name || 'Error'));
    await use(errors);
    expect(errors, 'В браузере не должно быть необработанных исключений').toEqual([]);
  }, {auto: true}],
  mountComponent: async ({page}, use) => {
    await page.route('**/*', route => route.abort('blockedbyclient'));
    await use(async (bundle, beforeMount) => {
      await page.setContent('<!doctype html><html><body><main id="root"></main></body></html>');
      if (beforeMount) await beforeMount();
      await page.addScriptTag({content: bundle});
    });
  },
});

export {expect};
export type {Page};
