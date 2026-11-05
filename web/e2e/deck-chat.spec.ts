// The chat of a deck: a screenshot pasted into the field that starts the project and a file picked for it stand in the
// first message; the terminal of the chat opens from the panel's head.
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const PASSWORD = 'correct horse';
const PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==';

let rt: Runtime;

test.beforeAll(async () => {
  rt = await startRuntime({
    root: mkdtempSync(join(tmpdir(), 'hs-deck-chat-')),
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
    },
  });
});
test.afterAll(async () => {
  await rt?.stop();
});

test('a pasted screenshot and a file go with the brief and stand in the chat of the deck', async ({
  browser,
}) => {
  const page = await browser.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  await page.goto(`${rt.url}/agents/designly/new/slides`);
  const field = page.locator('.hx-dl__input');
  await field.fill('A deck about the survey');
  await field.evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const data = new DataTransfer();
    data.items.add(new File([bytes], 'screenshot.png', { type: 'image/png' }));
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, PNG);
  await expect(page.locator('.hx-deck-file__thumb')).toBeVisible();
  const pdf = join(mkdtempSync(join(tmpdir(), 'hs-deck-pdf-')), 'survey.pdf');
  writeFileSync(pdf, '%PDF-1.4\n%%EOF\n');
  await page.locator('.hx-dl__bar input[type=file]').first().setInputFiles(pdf);
  await expect(page.locator('.hx-deck-file', { hasText: 'survey.pdf' })).toBeVisible();
  await field.press('Enter');

  await expect(page.locator('.hx-deck-chat .hx-msg[data-kind=user]').first()).toBeVisible({
    timeout: 60_000,
  });
  const first = page.locator('.hx-deck-chat .hx-msg[data-kind=user]').first();
  await expect(first).toContainText('survey');
  const files = first.locator('.hx-msg-user__files:not(.hx-msg-user__refs)');
  await expect(files).toContainText('survey.pdf', { timeout: 30_000 });
  await expect(files).toContainText('screenshot.png');

  // Read back from the transcript once the page was opened again (the stand-in agent keeps no picture there).
  await page.waitForTimeout(10_000);
  await page.reload();
  await expect(files).toContainText('survey.pdf', { timeout: 60_000 });

  await page.locator('.hx-deck-ph .hx-header__term').click();
  await expect(page.locator('.hx-term[data-open=true]')).toBeVisible();
  await page.close();
});
