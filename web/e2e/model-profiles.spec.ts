import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

// The provider models and their thinking levels travel the whole path: the profile's key makes the agent ready, the
// phone's picker offers the vendor's models and stops, and the chosen pair reaches the real launcher (a fake CLI here).
let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime();
  page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill('correct horse');
  await setup.locator('input[name=confirm]').fill('correct horse');
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
  const saved = await page.request.put(`${rt.url}/api/v2/providers/kimi-code/key`, {
    data: { key: 'e2e-kimi-code-key' },
  });
  expect(saved.ok()).toBe(true);
  await page.reload();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('a phone picks a Kimi Code model and effort, and Claude launches with both', async () => {
  const modelButton = page.locator('.hx-model');
  await modelButton.click();
  await page.locator('.hx-picker__center').click();
  await page.locator('.hx-seg__item', { hasText: 'Kimi Code' }).click();

  const models = page.locator('.hx-options[data-active="true"]');
  await expect(models.getByRole('option')).toHaveText([
    /K3\.1 Pro/,
    /K3\.1 Pro HS/,
    /K3/,
    /K3 · 256K/,
    /Kimi for Coding/,
    /K2\.7 Code HS/,
  ]);

  await models.getByRole('option', { name: 'K3.1 Pro', exact: true }).click();
  const effort = page.getByRole('slider');
  await expect(effort).toHaveAttribute('aria-valuetext', 'High');
  await expect(effort.locator('.hx-effort__stop')).toHaveCount(3);
  await expect(effort.getByRole('button', { name: 'Low', exact: true })).toBeVisible();
  await expect(effort.getByRole('button', { name: 'High', exact: true })).toBeVisible();
  await expect(effort.getByRole('button', { name: 'Maximum', exact: true })).toBeVisible();
  await effort.getByRole('button', { name: 'Maximum', exact: true }).click();
  await expect(effort).toHaveAttribute('aria-valuetext', 'Maximum');
  await page.keyboard.press('Escape');

  await page.locator('.hx-composer__input').fill('kimi effort e2e');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: kimi effort e2e')).toBeVisible({
    timeout: 60_000,
  });
  await expect
    .poll(() => readFileSync(join(rt.home, 'claude-calls.log'), 'utf8'))
    .toContain('--model k3.1-pro --effort max');
});
