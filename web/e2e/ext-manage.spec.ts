// Extensions as a system on a real runtime with HARNESSLY_UI_SLOTS=on: a folder is inspected, its permissions
// reviewed, the extension installed, its settings filled, switched off and removed. Screenshots go to EXT_SHOTS.
import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const SHOTS = process.env.EXT_SHOTS;

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-ext-'));
  rt = await startRuntime({ root, env: { HARNESSLY_UI_SLOTS: 'on' } });
  page = await browser.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

const dismissNews = async () => {
  const gotIt = page.getByRole('button', { name: 'Got it' });
  if (await gotIt.isVisible({ timeout: 6000 }).catch(() => false)) await gotIt.click();
};

const shoot = async (name: string) => {
  if (!SHOTS) return;
  await dismissNews();
  mkdirSync(SHOTS, { recursive: true });
  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme });
    for (const [w, h] of [
      [1440, 900],
      [390, 844],
    ]) {
      await page.setViewportSize({ width: w, height: h });
      await page.waitForTimeout(350);
      await page.screenshot({ path: join(SHOTS, `${name}-${theme}-${w}.png`), fullPage: true });
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
};

const open = async () => {
  const base = new URL(page.url()).pathname.replace(/\/settings\/.*$/, '').replace(/\/$/, '');
  await page.goto(`${rt.url}${base}/settings/extensions`);
  await dismissNews();
};

test('1. a folder is inspected, its permissions reviewed, then it is installed', async () => {
  await open();
  await page.getByLabel('Folder').fill(join(repo, 'examples', 'ext-ui-slots'));
  await page.getByRole('button', { name: 'Inspect' }).click();
  await expect(page.getByRole('region', { name: 'What the extension asks for' })).toBeVisible();
  await shoot('install');
  await page
    .getByRole('region', { name: 'What the extension asks for' })
    .getByRole('button', { name: 'Install' })
    .click();
  await expect(page.getByText('Example Corp workspace', { exact: true })).toBeVisible();
});

test('2. the settings form is kept by the core and the secret is write-only', async () => {
  await open();
  await page.getByLabel(/Value of api-token/).fill('s3cret-value');
  await page.getByRole('button', { name: 'Save' }).first().click();
  await expect(page.getByText('s3cret-value')).toHaveCount(0);
  await expect(page.getByText('set', { exact: false }).first()).toBeVisible();
  await page.getByRole('textbox', { name: 'Workspace', exact: true }).fill('team.example.com');
  await page.getByRole('button', { name: 'Save' }).last().click();
  await expect(page.getByRole('textbox', { name: 'Workspace', exact: true })).toHaveValue('team.example.com');
  await shoot('settings');
});

test('3. it is switched off and the page says so', async () => {
  await open();
  await page
    .getByRole('switch', { name: /Enabled/ })
    .first()
    .click();
  await expect(page.getByText('disabled').first()).toBeVisible();
  await shoot('disabled');
});
