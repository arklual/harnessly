import { defineConfig } from '@playwright/test';

// The browser tests start a standalone runtime of their own (e2e/global-setup.ts); build the page first (pnpm build).
// They drive the system Chrome unless PW_CHANNEL names another browser.
export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  expect: { timeout: 30_000 },
  workers: 1,
  reporter: [['list']],
  globalSetup: './e2e/global-setup.ts',
  use: {
    channel: process.env.PW_CHANNEL ?? 'chrome',
    headless: true,
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
  },
});
