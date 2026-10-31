import { test as base, expect, type Locator } from '@playwright/test';

// These are real browser requests to the Go/MariaDB service, never mocked APIs.
// Restrict all traffic to this loopback origin; providers and map tiles are not
// part of this smoke. Do not persist cookies, passwords, traces or screenshots.
export const test = base.extend<{ browserErrors: string[]; activate: (target: Locator) => Promise<void> }>({
  activate: async ({ isMobile }, use) => {
    // click() sends mouse/hover events even in a mobile browser context.
    // Touch navigation must exercise the component's real pointerType branch.
    await use(async target => {
      if (isMobile) await target.tap();
      else await target.click();
    });
  },
  browserErrors: [async ({ page, context, baseURL }, use) => {
    const errors: string[] = [];
    page.on('pageerror', error => {
      // CI uses disposable test data. Preserve the error type and a redacted
      // diagnostic, never URLs, query strings, credentials or arbitrary payloads.
      const safeMessage = String(error.message)
        .replace(/https?:\\/\\/[^\\s]+/g, '[redacted URL]')
        .replace(/[A-Za-z0-9_\\-]{32,}/g, '[redacted token]').slice(0, 180);
      errors.push('Uncaught browser exception (' + error.name + '): ' + safeMessage);
    });
    await context.route('**/*', route => {
      const url = new URL(route.request().url());
      if (url.origin !== new URL(baseURL!).origin) {
        errors.push('Unexpected external request');
        return route.abort();
      }
      return route.continue();
    });
    await use(errors);
    expect(errors).toEqual([]);
  }, { auto: true }],
});

export { expect };
