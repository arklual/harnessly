import { defineConfig } from '@playwright/test';

// The hub's journey in a browser (HRN-761): e2e-hub/journey.spec.ts on a real hub, agent and runtime, one cell of the
// matrix per run (E2E_CELL=cloud|selfhosted|installer|dev), and e2e-hub/live.spec.ts, the read-only run against the
// hubs that are up (E2E_LIVE_* variables). The browser maps the machines' test domain (run.test) to loopback itself.
export default defineConfig({
  testDir: 'e2e-hub',
  timeout: 240_000,
  expect: { timeout: 20_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [['list'], ['html', { outputFolder: 'e2e-hub-report', open: 'never' }]],
  outputDir: 'e2e-hub-results',
  use: {
    channel: process.env.PW_CHANNEL ?? 'chrome',
    headless: true,
    locale: 'ru-RU',
    viewport: { width: 1440, height: 900 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: { args: ['--host-resolver-rules=MAP *.run.test 127.0.0.1'] },
  },
});
