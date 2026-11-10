// An app installed from a folder waits for the person's consent: its page /apps/<id> shows the consent card with
// «Allow», not a blank screen (React #301 there, a render that set state over and over).
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';

let rt: Runtime;
let cli: string;

test.beforeAll(async () => {
  const root = mkdtempSync(join(tmpdir(), 'hs-consent-'));
  rt = await startRuntime({ root });
  cli = process.env.HARNESSLY_BIN ?? join(root, 'harnessly');
  if (!process.env.HARNESSLY_BIN)
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  const dir = join(root, 'consent-app');
  mkdirSync(join(dir, 'harnessly'), { recursive: true });
  mkdirSync(join(dir, 'ui'), { recursive: true });
  writeFileSync(
    join(dir, 'harnessly', 'app.json'),
    JSON.stringify({
      id: 'example.consent',
      name: 'Consent check',
      description: 'A view that asks for storage',
      version: '0.1.0',
      harnessly: '>=0.16.0',
      trust: 'sandboxed',
      targets: ['app'],
      permissions: ['storage'],
      views: [{ id: 'main', title: 'Main', entry: 'ui/index.html', targets: ['app'] }],
    }),
  );
  writeFileSync(join(dir, 'ui', 'index.html'), '<!doctype html><p>hi</p>');
  execFileSync(cli, ['app', 'install', dir], {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
  });
});

test.afterAll(async () => {
  await rt?.stop();
});

test('the page of an app that waits for consent shows the consent card', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(String(e.stack ?? e)));
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  const base = new URL(page.url()).pathname.startsWith('/agents') ? '/agents' : '';
  await page.evaluate((to) => {
    window.history.pushState({}, '', to);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, `${base}/apps/example.consent`);
  const allow = page.getByRole('button', { name: /^(Allow|Разрешить)/ }).first();
  await expect(allow).toBeVisible({ timeout: 15_000 });
  // «Allow» starts the window in place.
  await allow.click();
  await expect(page.locator('iframe').first()).toBeAttached({ timeout: 30_000 });
  expect(errors).toEqual([]);
});
