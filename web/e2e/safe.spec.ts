import { expect, test } from '@playwright/test';
import { ask, mockSafe, safeProject, safeReady, safeShot } from './safe-rig';

test('ordinary asks show the origin and submit only to the ask-specific put route', async ({ page }) => {
  const state = await mockSafe(page);
  state.asks = [ask()];
  await safeReady(page);
  const dialog = page.getByRole('dialog', { name: 'An agent asks for «release-token»', exact: true });
  await expect(dialog.getByText('https://example.org', { exact: true })).toBeVisible();
  await safeShot(page, 'safe-ask-origin');
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await safeShot(page, 'safe-ask-mobile');
  const field = dialog.getByLabel('Value of «release-token»');
  await field.fill('fake fixture value');
  await dialog.getByRole('button', { name: 'Put in the safe', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.calls.filter((call) => call.method === 'POST')).toEqual([
    {
      method: 'POST',
      path: '/api/platform/secret-asks/sa_fixture/put',
      body: JSON.stringify({ value: 'fake fixture value' }),
    },
  ]);
  expect(state.calls.some((call) => call.method === 'PUT')).toBe(false);
});

test('project grant cards have Allow and Decline and never mount a value input', async ({ page }) => {
  const state = await mockSafe(page);
  await safeReady(page);
  const project = await safeProject(page, 'Release access project');
  state.asks = [ask({ grant: true, project })];
  const dialog = page.getByRole('dialog', {
    name: 'Allow project Release access project to use release-token?',
    exact: true,
  });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('input')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Decline', exact: true })).toBeEnabled();
  await safeShot(page, 'safe-project-grant');
  await dialog.getByRole('button', { name: 'Allow', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.calls.find((call) => call.method === 'POST')).toEqual({
    method: 'POST',
    path: '/api/platform/secret-asks/sa_fixture/grant',
    body: null,
  });
});

test('Safe edits origins and project choices with PATCH, without rotating the value', async ({ page }) => {
  const state = await mockSafe(page);
  await safeReady(page);
  const project = await safeProject(page, 'Metadata choices project');
  const base = await page.locator('meta[name="api-base"]').getAttribute('content');
  await page.goto(`${process.env.E2E_URL}${base ?? ''}/settings/safe`);
  const row = page.locator('.hx-safe').filter({ hasText: 'release-token' });
  await row.getByRole('button', { name: 'Edit access', exact: true }).click();
  await row.getByRole('textbox', { name: /Origins/ }).fill('https://new.example.org');
  await row.getByLabel('Metadata choices project', { exact: true }).check();
  await safeShot(page, 'safe-metadata');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await safeShot(page, 'safe-metadata-mobile');
  await row
    .getByRole('group', { name: 'Edit access', exact: true })
    .getByRole('button', { name: 'Save', exact: true })
    .click();
  await expect(row.getByRole('textbox', { name: /Origins/ })).toHaveCount(0);
  expect(state.calls.find((call) => call.method === 'PATCH')).toEqual({
    method: 'PATCH',
    path: '/api/platform/secrets/release-token',
    body: JSON.stringify({ origins: ['https://new.example.org'], projects: [project] }),
  });
  expect(state.calls.some((call) => call.method === 'PUT')).toBe(false);
});

test('origin-only grant card requests site permission without any value input', async ({ page }) => {
  const state = await mockSafe(page);
  state.asks = [ask({ originGrant: true })];
  await safeReady(page);
  const dialog = page.getByRole('dialog', {
    name: 'Allow release-token on site https://example.org?',
    exact: true,
  });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Site', { exact: true })).toBeVisible();
  await expect(dialog.locator('input, textarea')).toHaveCount(0);
  await safeShot(page, 'safe-origin-grant');
  await dialog.getByRole('button', { name: 'Allow', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.calls.find((call) => call.method === 'POST')).toEqual({
    method: 'POST',
    path: '/api/platform/secret-asks/sa_fixture/grant',
    body: null,
  });
});
