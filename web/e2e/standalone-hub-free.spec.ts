// A runtime with no hub (RUNTIME_GATE_UPSTREAM is empty, the built-in store) serves Sinopia whole: the home
// opens with cards or its empty state and no error, a design is created and opened, and ⌘K opens and finds it.
// Part of e2e:smoke; it waits on page state only, never on a sleep.
import { expect, type Page, test } from '@playwright/test';

import { RELEASES } from '../app/next/features/whats-new/releases';
import { startRuntime, type Runtime } from './runtime';

const PASSWORD = 'correct horse';
const TITLE = 'Smoke without a hub zebra';

test.describe.configure({ mode: 'serial', timeout: 120_000 });

let rt: Runtime;
let base = '';
let designId = '';

test.beforeAll(async () => {
  test.setTimeout(120_000);
  rt = await startRuntime({ env: { RUNTIME_LOCALE: 'en', RUNTIME_WEB_FEATURES: 'chats,design' } });
});
test.afterAll(async () => {
  await rt?.stop();
});

const signIn = async (page: Page) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  // «What's new» is a modal: seen already, so ⌘K reaches the page.
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
  await page.addLocatorHandler(page.getByRole('button', { name: /^(Got it|Понятно)$/ }), (b) => b.click());
  await page.goto(`${rt.url}/`);
  // The first test sets the password; the tests after it sign in with it.
  const setup = page.getByTestId('setup-form');
  const login = page.getByTestId('signin-form');
  await expect(setup.or(login)).toBeVisible();
  if (await setup.isVisible()) {
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
  } else {
    await login.locator('input[name=password]').fill(PASSWORD);
    await login.locator('button[type=submit]').click();
  }
  await expect(page.locator('.hx-hello')).toBeVisible();
  base = new URL(page.url()).pathname.replace(/\/$/, '');
};

test('1. the Sinopia home opens without a hub: cards or the empty state, no error', async ({ page }) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/designly`);
  await expect(page.locator('.hx-dl-card, .hx-dl__empty').first()).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('2. a design is created and opens', async ({ page }) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/designly`);
  const made = await page.evaluate(async (title) => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        title,
        brief: 'smoke',
        kind: 'slides',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return { ok: res.ok, body: (await res.json()) as { id?: string } };
  }, TITLE);
  expect(made.ok).toBe(true);
  designId = made.body.id ?? '';
  expect(designId).not.toBe('');
  await page.goto(`${rt.url}${base}/designly/${designId}`);
  await expect(page).toHaveURL(new RegExp(`/designly/${designId}`));
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('3. ⌘K opens the search and finds the design', async ({ page }) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/designly`);
  await expect(page.locator('.hx-dl-card').first()).toBeVisible();
  await page.keyboard.press('ControlOrMeta+k');
  const box = page.locator('.hx-srch__box');
  await expect(box).toBeVisible();
  await box.locator('input[type=search]').fill('zebra');
  await expect(box.getByRole('option', { name: /zebra/ }).first()).toBeVisible();
});
