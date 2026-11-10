import { expect, test } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

// The read-only run against hubs that are up (HRN-761): nothing is created, changed or deleted. The addresses and the
// test account come from CI variables only, never from the code:
//   E2E_LIVE_HUBS       space-separated hub addresses (https://hub.example.com ...)
//   E2E_LIVE_COOKIE     the value of the test account's session cookie (masked in CI). An account that signs in by a
//                       provider or by a code sent to a mailbox cannot be driven without a secret, so the signed-in
//                       part runs only when it is set; every other test of the file needs no account.
// Without E2E_LIVE_HUBS the whole file is skipped.

const hubs = (process.env.E2E_LIVE_HUBS ?? '').split(/\s+/).filter(Boolean);
const cookie = process.env.E2E_LIVE_COOKIE ?? '';
const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots', 'live');

test.skip(hubs.length === 0, 'E2E_LIVE_HUBS is not set');

for (const [i, hub] of hubs.entries()) {
  test.describe(`live hub #${i + 1}`, () => {
    test('the sign-in page loads and the hub answers', async ({ page }) => {
      const health = await page.request.get(`${hub}/api/health`);
      expect(health.ok()).toBeTruthy();
      const config = await (await page.request.get(`${hub}/api/config`)).json();
      expect(config).toHaveProperty('signup');
      const errors: string[] = [];
      page.on('pageerror', (e) => errors.push(e.message));
      await page.goto(`${hub}/signin`);
      await expect(page.locator('input[type=email], .hxa__card').first()).toBeVisible();
      mkdirSync(shotsDir, { recursive: true });
      await page.screenshot({ path: join(shotsDir, `hub-${i + 1}-signin.png`) });
      expect(errors).toEqual([]);
    });

    test('signed in: the machines and the organizations are listed', async ({ browser }) => {
      test.skip(!cookie, 'E2E_LIVE_COOKIE is not set: the signed-in part is skipped');
      const ctx = await browser.newContext();
      await ctx.addCookies([
        { name: hub.startsWith('https:') ? '__Host-hl_session' : 'hl_session', value: cookie, url: hub },
      ]);
      const page = await ctx.newPage();
      const me = await page.request.get(`${hub}/api/me`);
      expect(me.ok()).toBeTruthy();
      await page.goto(`${hub}/machines`);
      await expect(page.locator('body')).not.toContainText('Не удалось');
      await page.screenshot({ path: join(shotsDir, `hub-${i + 1}-machines.png`) });
      await ctx.close();
    });
  });
}
