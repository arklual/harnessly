// The UI slots of apps on a real runtime with HARNESSLY_UI_SLOTS=on: the example package fills the Extensions
// page of Settings (a settings view and an onboarding step) and puts a button at each machine, and its brand reaches the
// page. (A crashing view is held by the boundary: see ui-slot.test.tsx.) Screenshots go to SLOT_SHOTS when it is set.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const SHOTS = process.env.SLOT_SHOTS;

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-slots-'));
  rt = await startRuntime({ root, env: { HARNESSLY_UI_SLOTS: 'on' } });
  let cli = process.env.HARNESSLY_BIN;
  if (!cli) {
    cli = join(root, 'harnessly');
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  }
  execFileSync(cli, ['app', 'install', join(repo, 'examples', 'ext-ui-slots')], {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
    stdio: 'inherit',
  });
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

const at = (p: string) => {
  const base = new URL(page.url()).pathname.replace(/\/settings\/.*$/, '').replace(/\/$/, '');
  return `${rt.url}${base}${p}`;
};

// The "What's new" popover of the app would cover the slots in the pictures.
const dismissNews = async () => {
  const gotIt = page.getByRole('button', { name: 'Got it' });
  if (await gotIt.isVisible({ timeout: 6000 }).catch(() => false)) await gotIt.click();
  await expect(gotIt).toBeHidden();
};

const shoot = async (name: string) => {
  if (!SHOTS) return;
  await dismissNews();
  mkdirSync(SHOTS, { recursive: true });
  await page.screenshot({ path: join(SHOTS, `${name}.png`) });
};

test('1. the example fills the Extensions page, finishes its onboarding step and brands the page', async () => {
  await page.goto(at('/settings/extensions'));
  await dismissNews();
  await expect(page).toHaveTitle('Example Corp Agents');
  await expect(page.locator('.hx-set__nav-item', { hasText: 'Extensions' })).toBeVisible();
  const settings = page.frameLocator('.hx-uislot[data-slot=settings] iframe');
  await expect(settings.getByLabel('Workspace address')).toBeVisible({ timeout: 30_000 });
  const step = page.frameLocator('.hx-uislot[data-slot=onboarding] iframe');
  await expect(step.getByText('Connect your Example Corp account')).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('.hx-set__foot')).toContainText('0 of 1');
  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme });
    for (const [w, h] of [
      [1440, 900],
      [390, 844],
    ]) {
      await page.setViewportSize({ width: w, height: h });
      await page.waitForTimeout(400);
      await shoot(`extensions-${theme}-${w}`);
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await step.getByLabel('Workspace address').fill('team.example.com');
  await step.getByRole('button', { name: 'Continue' }).click();
  await expect(page.locator('.hx-set__foot')).toContainText('1 of 1');
});

test('2. a button of the package stands at the machine and knows which machine it is', async () => {
  await page.goto(at('/settings/machines'));
  const button = page.frameLocator('.hx-uislot[data-slot=object-action] iframe').getByRole('button');
  await expect(button).toContainText('Open', { timeout: 30_000 });
  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme });
    for (const [w, h] of [
      [1440, 900],
      [390, 844],
    ]) {
      await page.setViewportSize({ width: w, height: h });
      await page.locator('.hx-uislot-actions').first().scrollIntoViewIfNeeded();
      await page.waitForTimeout(400);
      await shoot(`machine-action-${theme}-${w}`);
    }
  }
});
