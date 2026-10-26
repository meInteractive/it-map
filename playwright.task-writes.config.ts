import { defineConfig } from '@playwright/test';
import path from 'node:path';
import base from './playwright.config';

if (process.env.GITHUB_ACTIONS !== 'true' || process.env.ITMAP_TASK_BROWSER_WRITES !== '1') {
  throw new Error('Task browser writes require the guarded disposable Go CI parent.');
}
export default defineConfig({
  ...base,
  testDir: './tests/browser-task-writes',
  outputDir: path.join(process.env.RUNNER_TEMP || '/tmp', 'itmap-task-browser-results'),
});
