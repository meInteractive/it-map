import {defineConfig} from '@playwright/test';
import path from 'node:path';
import base from './playwright.config';

if (process.env.GITHUB_ACTIONS !== 'true' || process.env.ITMAP_CHANNEL_BROWSER_WRITES !== '1') {
  throw new Error('Записи каналов разрешены только внутри одноразовой CI-БД');
}
export default defineConfig({
  ...base,
  testDir:'./tests/browser-channels',
  outputDir:path.join(process.env.RUNNER_TEMP || '/tmp','itmap-channels-browser-results'),
});
