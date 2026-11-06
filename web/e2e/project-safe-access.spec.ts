import { expect, test } from '@playwright/test';
import { mockSafe, safeProject, safeReady, safeShot } from './safe-rig';

test('project Access lists available secrets and opens their Safe access editor', async ({ page }) => {
  const state = await mockSafe(page);
  await page.route('**/api/agent-browser/profiles?*', (route) =>
    route.fulfill({ json: { profiles: [], projects: {}, asks: [], noSandbox: false } }),
  );
  await safeReady(page);
  const name = 'Project access fixture';
  const project = await safeProject(page, name);
  state.secret.projects = [project];
  const row = page.locator('.hx-sidebar [data-drop="project"]').filter({ hasText: name });
  await row.getByRole('button', { name: /^Settings of/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Project settings', exact: true });
  const access = dialog.getByRole('region', { name: 'Access', exact: true });
  await expect(access.getByRole('group', { name: 'Browser access', exact: true })).toBeVisible();
  const secrets = access.getByRole('group', { name: 'Secrets', exact: true });
  await expect(secrets.getByText('release-token', { exact: true })).toBeVisible();
  await expect(secrets.getByText('Origins: https://example.org', { exact: true })).toBeVisible();
  await expect(secrets.getByText('Latest uses', { exact: true })).toBeVisible();
  await expect(secrets.getByText(/Release notes · https:\/\/example.org/)).toBeVisible();
  await expect(secrets.locator('input')).toHaveCount(0);
  await secrets.scrollIntoViewIfNeeded();
  await safeShot(page, 'project-safe-access');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await secrets.scrollIntoViewIfNeeded();
  const save = dialog.getByRole('button', { name: 'Save', exact: true });
  await expect(save).toBeInViewport();
  await safeShot(page, 'project-safe-access-mobile');
  await secrets.getByRole('link', { name: 'Edit access', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page).toHaveURL(/\/settings\/safe\?secret=release-token$/);
  await expect(page.getByRole('textbox', { name: /Origins/ })).toHaveValue('https://example.org');
  expect(state.calls.every((call) => call.method === 'GET')).toBe(true);
});
