import { defineConfig } from '@playwright/test';
import path from 'node:path';
import base from './playwright.config';

if (process.env.GITHUB_ACTIONS !== 'true' || process.env.ITMAP_COMPANY_BROWSER_WRITES !== '1') {
  throw new Error('Браузерные записи CRM разрешены только внутри одноразового CI');
}
export default defineConfig({
  ...base,
  testDir: './tests/browser-company-flags',
  outputDir: path.join(process.env.RUNNER_TEMP || '/tmp', 'itmap-company-flags-browser-results'),
});
