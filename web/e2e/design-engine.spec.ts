/// <reference lib="dom" />
import { mkdtempSync, copyFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { expect, test } from '@playwright/test';

// Designs need nothing set up: a machine without the vendor's engine says so in Settings only, and a board draws on the
// built-in engine.

const PASSWORD = 'correct horse';
const FETCH = 'harnessly design engine fetch';

const board = `<!doctype html>
<html><head><meta charset="utf-8"><script src="./support.js"></script></head>
<body>
<x-dc>
<helmet><style>html,body{margin:0}</style></helmet>
<main class="board">
  <h1 id="title">{{ title }}</h1>
  <sc-if value="{{ open }}"><p id="more">More</p></sc-if>
  <ul><sc-for list="{{ items }}" as="item"><li>{{ item }}</li></sc-for></ul>
  <button id="toggle" onClick="{{ toggle }}">Toggle</button>
</main>
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{"title":{"type":"string","default":"Hello"}}'>
class Component extends DCLogic {
  state = { open: false };
  toggle = () => this.setState({ open: !this.state.open });
  renderVals() {
    return { title: this.props.title, open: this.state.open, items: ['one', 'two'], toggle: this.toggle };
  }
}
</script>
</body></html>
`;

test('a board draws on the built-in engine', async ({ page }) => {
  const dir = mkdtempSync(join(tmpdir(), 'hs-own-engine-'));
  copyFileSync(resolve('../internal/design/ownengine/support.js'), join(dir, 'support.js'));
  writeFileSync(join(dir, 'board.dc.html'), board);
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(`file://${join(dir, 'board.dc.html')}`);
  await expect(page.locator('#title')).toHaveText('Hello');
  await expect(page.locator('li')).toHaveText(['one', 'two']);
  await expect(page.locator('#more')).toHaveCount(0);
  await page.locator('#toggle').click();
  await expect(page.locator('#more')).toBeVisible();
  expect(errors).toEqual([]);
});

test('no notice on the design page; Settings says which engine and copies the command', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto(process.env.E2E_URL ?? '');
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();

  await page.goto(`${process.env.E2E_URL}/agents/designly`);
  await expect(page.locator('.hx-dl__hero')).toBeVisible();
  await expect(page.getByText(FETCH)).toHaveCount(0);
  await expect(page.getByText('board engine')).toHaveCount(0);

  // A new design on a machine that has fetched nothing.
  const made = await page.evaluate(async () => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'Landing',
        brief: 'A landing page',
        kind: 'slides',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return { status: res.status, body: await res.text() };
  });
  expect(made.status, made.body).toBeLessThan(300);

  await page.goto(`${process.env.E2E_URL}/agents/settings/design`);
  await expect(page.getByText('Built-in', { exact: true })).toBeVisible();
  await expect(page.locator('.hx-set__command-text')).toHaveText(FETCH);
  await expect(page.getByText('Chromium', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Copy' }).click();
  await expect(page.getByRole('button', { name: 'Copied' })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(FETCH);
  if (process.env.E2E_SHOT) await page.screenshot({ path: process.env.E2E_SHOT });

  // A machine whose enforced settings refuse making a canvas: the engine is named, the command that cannot work is not.
  await page.route('**/design/engine*', async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, json: { ...(await res.json()), fetch: '' } });
  });
  await page.reload();
  await expect(page.getByText('Built-in', { exact: true })).toBeVisible();
  await expect(page.locator('.hx-set__command-text')).toHaveCount(0);
});
