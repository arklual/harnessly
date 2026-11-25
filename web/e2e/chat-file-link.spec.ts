import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { SEEN_ALL } from '../app/next/features/whats-new/seen';
import { startRuntime } from './runtime';

test('a report linked from an agent reply opens in Files (HRN-1745)', async ({ page }) => {
  const rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
  try {
    const reports = join(rt.home, 'project', 'reports');
    mkdirSync(reports);
    writeFileSync(join(reports, 'weekly.md'), '# Fixture report\n');
    await page.addInitScript((seen) => localStorage.setItem('hx:whats-new', seen), SEEN_ALL);
    await page.goto(rt.url);
    const setup = page.getByTestId('setup-form');
    await setup.locator('input[name=password]').fill('correct horse');
    await setup.locator('input[name=confirm]').fill('correct horse');
    await setup.locator('button[type=submit]').click();
    const input = page.locator('.hx-composer__input');
    await input.fill('See [Full report](reports/weekly.md).');
    await input.press('Enter');

    await page.locator('.hx-feed').getByRole('button', { name: 'Full report' }).click();
    const pane = page.locator('.hx-pane');
    await expect(pane.locator('.hx-fed .cm-content')).toContainText('# Fixture report');
    await page.screenshot({ path: test.info().outputPath('opened-report.png') });
  } finally {
    await rt.stop();
  }
});
