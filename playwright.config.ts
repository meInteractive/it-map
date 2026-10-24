import { defineConfig } from '@playwright/test';
import path from 'node:path';

const baseURL = process.env.ITMAP_BROWSER_URL || 'http://127.0.0.1:18080';
const target = new URL(baseURL);
if (target.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(target.hostname) ||
    target.username || target.password || target.pathname !== '/' || target.search || target.hash) {
  throw new Error('Browser smoke requires loopback HTTP and synthetic CI data.');
}

export default defineConfig({
  testDir: './tests/browser',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 30_000,
  reporter: process.env.GITHUB_ACTIONS ? [['list'], ['github']] : 'list',
  outputDir: path.join(process.env.RUNNER_TEMP || '/tmp', 'itmap-browser-results'),
  use: {
    baseURL,
    browserName: 'chromium',
    serviceWorkers: 'block',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    launchOptions: { executablePath: process.env.ITMAP_BROWSER_EXECUTABLE },
  },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true } },
  ],
});
