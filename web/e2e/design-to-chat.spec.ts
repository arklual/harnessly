// «К чату» of a design's chat is that chat's choice: after another chat, which keeps its own view (the
// design), and after a reload, the chat comes back as a plain chat, not as the design on the whole screen.
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const PASSWORD = 'correct horse';
const SHOTS = process.env.E2E_SHOTS ?? '';

let rt: Runtime;

test.beforeAll(async () => {
  rt = await startRuntime({
    root: mkdtempSync(join(tmpdir(), 'hs-design-to-chat-')),
    env: { RUNTIME_WEB_FEATURES: 'chats,design' },
  });
});
test.afterAll(async () => {
  await rt?.stop();
});

const shot = async (page: Page, name: string) => {
  if (SHOTS) await page.screenshot({ path: join(SHOTS, `${name}.png`) });
};

const newChat = async (page: Page, text: string): Promise<string> => {
  await page.goto(`${rt.url}/`);
  await page.locator('.hx-composer__input').fill(text);
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page).toHaveURL(/\/c\/[^/?]+$/);
  return decodeURIComponent(new URL(page.url()).pathname.split('/').pop() ?? '');
};

test("«К чату» stays the chat's view after another chat and a reload", async ({ browser }) => {
  test.setTimeout(180_000);
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();

  // A design made with its chat: that chat is A.
  const made = await page.evaluate(async () => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'alpha landing',
        brief: 'alpha landing',
        kind: 'site',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return (await res.json()) as { id?: string; conv?: string };
  });
  expect(made.conv).toMatch(/^tab-/);
  const a = made.conv ?? '';
  const design = made.id ?? '';
  const b = await newChat(page, 'beta chat');
  // The app's addresses stand under the workspace's base.
  const base = page.url().replace(/\/c\/[^/]+$/, '');

  // Both chats edit the design: each is the design with its chat.
  const toChat = page.locator('.hx-site__to-chat');
  await page.goto(`${base}/designly/${design}?chat=${b}&edit=1`);
  await expect(toChat).toBeVisible({ timeout: 30_000 });
  await page.goto(`${base}/designly/${design}?chat=${a}&edit=1`);
  await expect(toChat).toBeVisible({ timeout: 30_000 });
  // The head names the chat, with no switch of the design's chats (the sidebar and ⌘K switch them).
  await expect(page.locator('.hx-site__chat-title')).toHaveText('alpha landing');
  await expect(page.locator('.hx-chatswitch')).toHaveCount(0);
  await shot(page, '1-a-design');

  // «К чату» in A: the plain chat.
  const gotIt = page.getByRole('button', { name: 'Got it' });
  if (await gotIt.isVisible()) await gotIt.click();
  await toChat.click();
  await expect(page).toHaveURL(new RegExp(`/c/${a}$`));
  await expect(page.locator('.hx-sidebar')).toBeVisible();
  await page.waitForTimeout(1000);
  await shot(page, '2-a-chat');

  // B from the sidebar: B keeps its own view, the design.
  await page.locator('.hx-sidebar__list').getByText('beta chat').first().click();
  await expect(page).toHaveURL(new RegExp(`/designly/${design}\\?.*chat=${b}`));
  await expect(toChat).toBeVisible({ timeout: 30_000 });
  await shot(page, '3-b-design');

  // Back to A, from the sidebar of the start page (the design's rail folds it): still the plain chat.
  await page
    .locator('.hx-nav__item')
    .filter({ has: page.locator('[data-i="square-pen"]') })
    .first()
    .click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  await page.waitForTimeout(800);
  await shot(page, '3b-home');
  // The chats' list (Sinopia Design's folded group names the design too).
  await page.locator('.hx-sidebar__list').getByText('alpha landing').first().click();
  await expect(page).toHaveURL(new RegExp(`/c/${a}$`));
  await page.waitForTimeout(1500);
  await expect(page).toHaveURL(new RegExp(`/c/${a}$`));
  await shot(page, '4-a-back');

  // And after a reload.
  await page.reload();
  await expect(page.locator('.hx-app')).toBeVisible();
  await page.waitForTimeout(1500);
  await expect(page).toHaveURL(new RegExp(`/c/${a}$`));
  await shot(page, '5-a-reload');

  // B's tab, the design edited with its chat, is named by the chat, not by the design.
  const strip = page.locator('.hx-gtabs__list');
  await page.locator('.hx-sidebar__list').getByText('beta chat').first().click();
  await expect(page).toHaveURL(new RegExp(`/designly/${design}\\?.*chat=${b}`));
  const bTab = strip.locator('.hx-gtab[aria-selected="true"] .hx-gtab__name');
  await expect(bTab).toHaveText('beta chat');
  await shot(page, '6-b-tab');
  // A rename of the chat renames its tab.
  await page
    .locator('.hx-nav__item')
    .filter({ has: page.locator('[data-i="square-pen"]') })
    .first()
    .click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  const row = page.locator('.hx-sidebar__list .hx-item', { hasText: 'beta chat' }).first();
  await row.hover();
  await row.locator('.hx-item__menu').first().click();
  await page.getByText('Rename', { exact: true }).first().click();
  const field = page.getByLabel('Chat name');
  await field.fill('gamma renamed');
  await field.press('Enter');
  await expect(strip.locator('.hx-gtab__name', { hasText: 'gamma renamed' })).toHaveCount(1);
  await shot(page, '7-renamed');

  // On a phone the design's chat still opens with its head.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${base}/designly/${design}?chat=${b}&edit=1`);
  await page.waitForTimeout(2500);
  await expect(page.locator('.hx-chatswitch')).toHaveCount(0);
  await shot(page, '8-phone');
  await page.close();
});
