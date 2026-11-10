/// <reference lib="dom" />
import { expect, test } from '@playwright/test';

import { startRuntime } from './runtime';

// The fleet hub's chat move (HRN-773), seen from the page. The hub is stood in for by routes over
// /api/v2/chat-move/* (one runtime cannot be two machines behind a hub; the real answers are covered by the hub's own
// Go tests in internal/hub/proxy). The person opens the chat menu, sees the machines with their organization, the one
// that is away refused in words, and moves the chat.

const PASSWORD = 'correct horse';
const SHOTS = process.env.CHAT_MOVE_SHOTS ?? '.';

const targets = {
  source: { ok: true, project: false, dirty: false, busy: false },
  targets: [
    {
      machineId: 'machine-two',
      name: 'build',
      orgName: 'Globex',
      online: true,
      ok: true,
      checks: [],
      filesOk: true,
      agents: [
        {
          provider: 'claude',
          profile: '',
          short: 'Claude',
          models: [{ id: 'opus', label: 'Opus', default: true }],
        },
      ],
    },
    {
      machineId: 'machine-three',
      name: 'laptop',
      orgName: 'Initech',
      online: false,
      ok: false,
      code: 'machine_offline',
      checks: [],
      filesOk: false,
      agents: [],
    },
  ],
};

for (const [scheme, width, height] of [
  ['light', 1440, 900],
  ['dark', 1440, 900],
  ['light', 390, 844],
  ['dark', 390, 844],
] as const) {
  test(`move a chat to a machine of another organization, ${scheme} ${width}`, async ({ browser }) => {
    const rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
    const ctx = await browser.newContext({ colorScheme: scheme, viewport: { width, height } });
    const page = await ctx.newPage();
    let moved: Record<string, unknown> | null = null;
    await page.route(/\/api\/v2\/chat-move\/status/, (r) =>
      r.fulfill({ json: { enabled: true, moved: null } }),
    );
    await page.route(/\/api\/v2\/chat-move\/targets/, (r) => r.fulfill({ json: targets }));
    await page.route(/\/api\/v2\/chat-move(\?|$)/, async (r) => {
      moved = r.request().postDataJSON() as Record<string, unknown>;
      await r.fulfill({ json: { targetMachineId: 'machine-two', targetName: 'build', convId: 'tab-x' } });
    });
    try {
      await page.goto(`${rt.url}/`);
      const setup = page.getByTestId('setup-form');
      await setup.locator('input[name=password]').fill(PASSWORD);
      await setup.locator('input[name=confirm]').fill(PASSWORD);
      await setup.locator('button[type=submit]').click();
      await expect(page.locator('.hx-composer__input')).toBeVisible();
      const gotIt = page.getByRole('button', { name: 'Got it' });
      if (await gotIt.isVisible({ timeout: 3000 }).catch(() => false)) await gotIt.click();
      await page.locator('.hx-composer__input').fill('carry me over');
      await page.locator('.hx-composer__input').press('Enter');
      await expect(page.locator('.hx-feed').getByText('Fake agent reply: carry me over')).toBeVisible({
        timeout: 60_000,
      });
      const news = page.getByRole('button', { name: 'Got it' });
      if (await news.isVisible({ timeout: 8000 }).catch(() => false)) await news.click();
      if (width < 800) {
        // On a phone the sidebar is a drawer; its rows have the same menu.
        await page.getByRole('button', { name: 'Menu', exact: true }).click();
      }
      await page.locator('.hx-sidebar').getByText('carry me over').first().hover();
      await page.locator('.hx-sidebar [aria-haspopup="menu"]').first().click({ force: true });
      await page.getByRole('menuitem', { name: 'Move to a machine…' }).click();
      await expect(page.getByText('Globex')).toBeVisible();
      await expect(page.getByText('Initech')).toBeVisible();
      await expect(page.getByText('Offline')).toBeVisible();
      await page.screenshot({ path: `${SHOTS}/chat-move-${scheme}-${width}.png` });
      await page.getByRole('radio', { name: /build/ }).click();
      await page.getByRole('button', { name: 'Continue' }).click();
      await expect.poll(() => moved?.targetMachineId).toBe('machine-two');
    } finally {
      await ctx.close();
      await rt.stop();
    }
  });
}
