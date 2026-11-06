import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

// An agent's own widget travels the whole path: the agent runs the real `harnessly widget render`, the machine keeps
// the widget, the feed draws it under the command's step in the sandboxed frame, and the same id shown again changes
// that widget where it stands instead of adding a second one.
let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime();
  page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
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

test('an agent draws its own widget in the feed and changes it by the same id', async () => {
  const input = page.locator('.hx-composer__input');
  await input.fill('show a widget 7');
  await input.press('Enter');
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: show a widget 7')).toBeVisible({
    timeout: 60_000,
  });
  const card = page.locator('.hx-feed .hx-appcard iframe');
  await expect(card).toHaveCount(1, { timeout: 20_000 });
  await expect(page.frameLocator('.hx-feed .hx-appcard iframe').getByText('Sales 7')).toBeVisible({
    timeout: 20_000,
  });

  await page.locator('.hx-composer__input').fill('now the widget 9');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: now the widget 9')).toBeVisible({
    timeout: 60_000,
  });
  // The second step changes «sales» where it first stood: still one widget, now with the new number.
  await expect(page.frameLocator('.hx-feed .hx-appcard iframe').getByText('Sales 9')).toBeVisible({
    timeout: 20_000,
  });
  await expect(card).toHaveCount(1);
});
