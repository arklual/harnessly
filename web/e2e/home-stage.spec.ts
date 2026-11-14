// A view of the start page that takes the whole stage on a real runtime with HARNESSLY_UI_SLOTS=on: the
// example package's page of the day fills the stage under the field and puts its own greeting over it. Screenshots go to
// SLOT_SHOTS when it is set.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, test } from '@playwright/test';

import { startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const SHOTS = process.env.SLOT_SHOTS;

test('the page of the day takes the start page under the field, with its own greeting', async ({
  browser,
}) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-home-stage-'));
  const rt = await startRuntime({ root, env: { HARNESSLY_UI_SLOTS: 'on' } });
  try {
    let cli = process.env.HARNESSLY_BIN;
    if (!cli) {
      cli = join(root, 'harnessly');
      execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
    }
    execFileSync(cli, ['app', 'install', join(repo, 'examples', 'ext-home-stage')], {
      env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
      stdio: 'inherit',
    });
    const page = await browser.newPage();
    await page.goto(`${rt.url}/`);
    const setup = page.getByTestId('setup-form');
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
    await expect(page.locator('.hx-app')).toBeVisible();
    // «Что нового» and the welcome of the lessons come up once on a fresh browser; the person puts them away first.
    for (const close of ['.hx-news__ok', '.hx-learn-hello__x']) {
      const el = page.locator(close);
      await el.waitFor({ timeout: 10_000 }).then(
        () => el.click(),
        () => undefined,
      );
      await expect(el).toHaveCount(0);
    }
    // The machine's setup card of a fresh runtime stands over the greeting; the person hides it.
    const quiet = page.locator('.hx-setup__quiet');
    await quiet.waitFor({ timeout: 10_000 }).then(
      () => quiet.click(),
      () => undefined,
    );
    await expect(page.locator('.hx-setup')).toHaveCount(0);
    await expect(page.locator('.hx-stage[data-home=stage]')).toBeVisible({ timeout: 30_000 });
    const day = page.frameLocator('.hx-home-stage iframe');
    await expect(day.getByText('Call the venue about Friday')).toBeVisible({ timeout: 30_000 });
    await expect(page.locator('.hx-hello__title')).toHaveText('Three things for today');
    await expect(page.locator('.hx-hello__sub')).toHaveText('Two meetings, one review waits for you');
    // The field stays over the view and takes the person's words.
    const field = page.locator('.hx-dock .hx-composer__input');
    await expect(field).toBeVisible();
    const fieldBox = await page.locator('.hx-dock').boundingBox();
    const stageBox = await page.locator('.hx-home-stage').boundingBox();
    expect(fieldBox && stageBox && fieldBox.y + fieldBox.height <= stageBox.y + 4).toBe(true);
    if (SHOTS) {
      mkdirSync(SHOTS, { recursive: true });
      for (const theme of ['light', 'dark'] as const) {
        await page.emulateMedia({ colorScheme: theme });
        for (const [w, h] of [
          [1440, 900],
          [390, 844],
        ]) {
          await page.setViewportSize({ width: w, height: h });
          await page.waitForTimeout(500);
          await page.screenshot({ path: join(SHOTS, `home-stage-${theme}-${w}.png`) });
        }
      }
    }
    await page.close();
  } finally {
    await rt.stop();
  }
});
