// «Приложения» in the new look (HRN-769) and the runtime without tasks, MRs and help under the «hubFirst» flag
// (HRN-766). The apps are installed as a person does with the CLI; the shots go to E2E_SHOTS.
import { execFileSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const shots = process.env.E2E_SHOTS ?? tmpdir();

/** The server answers deep links with 404, so the page is moved the way its router does it. */
const go = (path: string) =>
  page.evaluate((to) => {
    window.history.pushState({}, '', to);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, path);

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-apps-'));
  rt = await startRuntime({ root, env: { RUNTIME_WEB_FEATURES: 'chats,design' } });
  let cli = process.env.HARNESSLY_BIN;
  if (!cli) {
    cli = join(root, 'harnessly');
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  }
  for (const app of ['standup', 'cashback']) {
    execFileSync(cli, ['app', 'install', join(repo, 'docs', 'apps', app)], {
      env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
      stdio: 'inherit',
    });
  }
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('1. the apps are cards in a grid; a card opens its app, the details open in a side sheet', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();

  await go('/apps');
  const cards = page.locator('.hx-appcard');
  await expect(cards).toHaveCount(2);
  const gotIt = page.getByRole('button', { name: 'Got it' });
  if (await gotIt.isVisible()) await gotIt.click();
  const LARGE = { width: 1440, height: 900 };
  const PHONE = { width: 390, height: 844 };
  const shoot = async (scheme: string, size: { width: number; height: number }) => {
    await page.setViewportSize(size);
    if (await gotIt.isVisible()) await gotIt.click();
    await expect(cards.first()).toBeVisible();
    await page.screenshot({ path: join(shots, `apps-${scheme}-${size.width}.png`) });
    await page.locator('.hx-appcard__more').first().click();
    const sheet = page.locator('.hx-appsheet');
    await expect(sheet).toBeVisible();
    await page.waitForTimeout(700);
    await page.screenshot({ path: join(shots, `apps-sheet-${scheme}-${size.width}.png`) });
    await page.keyboard.press('Escape');
    await expect(sheet).toHaveCount(0);
  };
  await shoot('light', LARGE);
  await shoot('light', PHONE);
  await page.setViewportSize(LARGE);
  await page.getByRole('button', { name: 'Dark theme' }).click();
  await shoot('dark', LARGE);
  await shoot('dark', PHONE);

  await page.setViewportSize({ width: 1440, height: 900 });
  await cards.first().locator('.hx-appcard__open').click();
  await expect(page).toHaveURL(/\/apps\/[^/]+/);
});

test('2. with «hubFirst» the tasks, merge requests and help go home; without it they open', async () => {
  await go('/tasks');
  await page.waitForTimeout(500);
  expect(new URL(page.url()).pathname).toBe('/tasks');

  await page.evaluate(() => localStorage.setItem('hl:beta', JSON.stringify({ hubFirst: true })));
  for (const path of ['/tasks', '/help', '/manage']) {
    await page.goto(`${rt.url}/`);
    await go(path);
    await expect(page.locator('.hx-hello')).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/agents/');
  }
  await page.evaluate(() => localStorage.removeItem('hl:beta'));
});
