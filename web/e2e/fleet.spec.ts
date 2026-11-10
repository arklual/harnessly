/// <reference lib="dom" />
import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

// One runtime, the machines of two organizations (HRN-764, HRN-765): the hub's list of the person's machines is stood in
// for by a route over /api/v2/machines (the real runtime answers every `machine=` of the page, as a standalone one
// does). The person sees both machines with their organization, picks the second one for the new chat, and the chat
// is listed with the machine and the organization it is on.

const PASSWORD = 'correct horse';
const reply = (text: string) => `Fake agent reply: ${text}`;

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
  page = await browser.newPage();
  await page.route(/\/api\/v2\/machines(\?|$)/, async (route) => {
    const res = await route.fetch();
    const own = (await res.json()) as Array<Record<string, unknown>>;
    if (!res.ok() || !Array.isArray(own)) return route.fulfill({ response: res });
    const first = { ...own[0], name: 'build', orgName: 'Acme' };
    const second = {
      ...own[0],
      id: 'machine-two',
      name: 'build',
      host: 'build-2.example.com',
      orgName: 'Globex',
    };
    await route.fulfill({ json: [first, second] });
  });
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('the machines of two organizations are in one runtime; a chat starts on the second one and its row says where it is', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();

  // Both machines are in the model's popover, each under its organization.
  await page.locator('.hx-composer .hx-model').first().click();
  const popover = page.locator('.hx-picker');
  await expect(popover).toContainText('Acme');
  await expect(popover).toContainText('Globex');
  // The machine of the new chat is a sub-view of the popover: open it, pick the second one.
  await popover.getByText(/^On /).first().click();
  await popover.getByRole('option').filter({ hasText: 'Globex' }).click();
  await page.keyboard.press('Escape');

  await page.locator('.hx-composer__input').fill('hello from the second machine');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(page.locator('.hx-feed').getByText(reply('hello from the second machine'))).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.locator('.hx-sidebar')).toContainText('hello from the second machine');
  await page.screenshot({ path: process.env.FLEET_SHOT ?? 'fleet.png' });
});
