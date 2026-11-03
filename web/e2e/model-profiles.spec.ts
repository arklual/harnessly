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
  // The note of what is new in this version covers the composer on a first visit.
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
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
  await expect(page.locator('.hx-effort__label')).toHaveText(['Low', 'High', 'Maximum']);
  await expect(page.locator('.hx-effort__label[data-chosen="true"]')).toHaveText('High');
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

// The picker holds any number of agents and the longest names: GLM's second preset with its flash model is the longest
// line, «Авто» the last agent. Nothing of either leaves the popover or the composer.
test('the picker keeps long agent and model names inside, and Auto comes on with a vendor key', async () => {
  for (const id of ['glm', 'glm-cn']) {
    const saved = await page.request.put(`${rt.url}/api/v2/providers/${id}/key`, {
      data: { key: `e2e-${id}-key` },
    });
    expect(saved.ok()).toBe(true);
  }
  const on = await page.request.put(`${rt.url}/api/v2/providers/auto/switch`, { data: { on: true } });
  expect(on.ok()).toBe(true);
  // A new chat: the first test's chat is still busy with its turn.
  await page.goto(`${rt.url}/agents/`);

  const shots = process.env.HX_E2E_SHOTS;
  await page.locator('.hx-model').click();
  await page.locator('.hx-picker__center').click();
  await page.locator('.hx-seg__item', { hasText: 'bigmodel.cn' }).click();
  await page
    .locator('.hx-options[data-active="true"]')
    .getByRole('option', { name: /GLM 5\.3 Flash/ })
    .click();
  await page.keyboard.press('Escape');
  await page.locator('.hx-model').click();

  const inside = (sel: string, box: string) =>
    page.evaluate(
      ([s, b]) => {
        const el = document.querySelector(s)!.getBoundingClientRect();
        const out = document.querySelector(b)!.getBoundingClientRect();
        return el.left >= out.left - 0.5 && el.right <= out.right + 0.5;
      },
      [sel, box] as const,
    );
  await expect(page.locator('.hx-picker__model')).toContainText('GLM 5.3 Flash');
  expect(await inside('.hx-picker__model', '.hx-picker')).toBe(true);
  expect(await inside('.hx-model', '.hx-composer')).toBe(true);
  if (shots) await page.screenshot({ path: `${shots}/picker-glm-cn.png` });

  await page.locator('.hx-picker__center').click();
  const auto = page.locator('.hx-seg__item', { hasText: 'Авто' });
  await auto.scrollIntoViewIfNeeded();
  await auto.click();
  const models = page.locator('.hx-options[data-active="true"]');
  await expect(models.getByRole('option')).toHaveText([/^Авто/, /Авто · быстро/]);
  for (const opt of await models.getByRole('option').all()) await expect(opt).toBeInViewport();
  if (shots) await page.screenshot({ path: `${shots}/picker-auto.png` });
  await models.getByRole('option').first().click();
  await page.keyboard.press('Escape');

  await page.locator('.hx-composer__input').fill('auto e2e');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: auto e2e')).toBeVisible({
    timeout: 60_000,
  });
  await expect.poll(() => readFileSync(join(rt.home, 'claude-calls.log'), 'utf8')).toContain('--model auto');

  await page.goto(`${rt.url}/agents/settings/accounts`);
  const card = page.locator('.hx-acct--switch');
  await expect(card.getByRole('switch', { name: 'Auto: GLM and Kimi Code' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await card.scrollIntoViewIfNeeded();
  if (shots) await card.screenshot({ path: `${shots}/settings-auto.png` });
});
