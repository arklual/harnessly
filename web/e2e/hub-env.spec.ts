import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

// Settings → Skills and MCP, «From the organization»: what the hub handed the machine, as the machine
// agent holds it in environment.json (the runtime reads that file), with the layer, the pin and the status of each.
// E2E_SHOTS=<dir> also saves the block's pictures: both themes, 1440 and 390 px wide.
let rt: Runtime;
let page: Page;

const held = {
  rev: 4,
  hash: 'e2e',
  items: [
    {
      itemId: 'sk1',
      kind: 'skill',
      name: 'Code review',
      layer: 'org',
      pinned: true,
      source: { secretRefs: ['TOKEN'] },
    },
    { itemId: 'sk2', kind: 'skill', name: 'Release notes', layer: 'team', pinned: false },
    { itemId: 'mc1', kind: 'mcp', name: 'Tracker', layer: 'tenant', pinned: true },
    { itemId: 'mc2', kind: 'mcp', name: 'Wiki search', layer: 'team', pinned: false },
    { itemId: 'mc3', kind: 'mcp', name: 'Metrics', layer: 'org', pinned: false },
    { itemId: 'rp1', kind: 'repo', name: 'Sample repo', layer: 'org', pinned: false },
  ],
  applied: [
    { itemId: 'sk1', status: 'ok' },
    { itemId: 'sk2', status: 'pending' },
    { itemId: 'mc1', status: 'ok' },
    { itemId: 'mc2', status: 'error', message: 'secret WIKI_TOKEN is not in the safe' },
  ],
};

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_HUB_ENV_UI: 'on' } });
  writeFileSync(join(rt.home, '.harnessly', 'environment.json'), JSON.stringify(held), { mode: 0o600 });
  page = await browser.newPage();
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill('correct horse');
  await setup.locator('input[name=confirm]').fill('correct horse');
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

const shots = process.env.E2E_SHOTS;

test('the skills and MCP the hub handed are listed with layer, pin, status and error; a pinned one has no switch', async () => {
  const base = new URL(page.url()).pathname.replace(/\/$/, '');
  for (const [width, height] of [
    [1440, 900],
    [390, 844],
  ] as const) {
    await page.setViewportSize({ width, height });
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(`${rt.url}${base}/settings/skills`);
      const block = page.getByTestId('hub-env');
      // Skills first: the pinned one is applied, the other waits for the machine.
      await expect(block.locator('.hx-ext')).toHaveCount(2);
      const review = block.locator('.hx-ext').filter({ hasText: 'Code review' });
      await expect(review).toContainText('organization');
      await expect(review).toContainText('pinned');
      await expect(review).toContainText('applied');
      await expect(block.locator('.hx-ext').filter({ hasText: 'Release notes' })).toContainText(
        'waiting for the machine',
      );
      // The repository is the machine's setup, not a tool of the person.
      await expect(block).not.toContainText('Sample repo');
      // The personal switch is off by default (RUNTIME_HUB_ENV_SWITCH): nothing can be switched.
      await expect(block.getByRole('switch')).toHaveCount(0);
      if (shots) {
        mkdirSync(shots, { recursive: true });
        await page.screenshot({ path: join(shots, `hub-env-skills-${scheme}-${width}.png`) });
      }
      await page.getByRole(width > 600 ? 'tab' : 'tab', { name: 'MCP servers' }).click();
      await expect(block.locator('.hx-ext')).toHaveCount(3);
      const wiki = block.locator('.hx-ext').filter({ hasText: 'Wiki search' });
      await expect(wiki).toContainText('not applied: secret WIKI_TOKEN is not in the safe');
      await expect(block.locator('.hx-ext').filter({ hasText: 'Tracker' })).toContainText('space');
      await expect(block.locator('.hx-ext').filter({ hasText: 'Metrics' })).toContainText(
        'waiting for the machine',
      );
      if (shots) await page.screenshot({ path: join(shots, `hub-env-mcp-${scheme}-${width}.png`) });
    }
  }
});
