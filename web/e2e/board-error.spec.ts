// A board that fails shows its error card with «Отправить в чат» (HRN-862): from the viewing mode the project switches to
// editing, the chat opens and the error goes there, so the person sees their message and the agent's answer. On a site
// and a deck (a video's scene has no such card: its own error line has no chat to send to). Runs with BOARD_ERROR_E2E set (a runtime of its own, the fake agent answers).
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const PASSWORD = 'correct horse';
const SLOW = Math.max(1, Number(process.env.TOUR_SLOW ?? 1));

test.skip(!process.env.BOARD_ERROR_E2E, 'needs BOARD_ERROR_E2E');
test.describe.configure({ mode: 'default', timeout: 180_000 * SLOW });

let rt: Runtime;
let base = '';

test.beforeAll(async () => {
  test.setTimeout(120_000 * SLOW);
  rt = await startRuntime({ env: { RUNTIME_LOCALE: 'ru', RUNTIME_WEB_FEATURES: 'chats,design' } });
});
test.afterAll(async () => {
  await rt?.stop();
});

const signIn = async (page: Page) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addLocatorHandler(page.getByRole('button', { name: /^(Got it|Понятно)$/ }), (b) => b.click());
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  base = new URL(page.url()).pathname.replace(/\/$/, '');
};

/** Makes the first board of a demo project throw a moment after it loads. */
const breakBoard = (project: string, board: string) => {
  const found = execFileSync('find', [rt.root, '-path', `*/${project}/project/${board}`], {
    encoding: 'utf8',
  })
    .split('\n')
    .filter(Boolean);
  expect(found.length, `${project}/${board} on disk`).toBeGreaterThan(0);
  for (const file of found) {
    const html = readFileSync(file, 'utf8');
    writeFileSync(
      file,
      html.replace(
        '</head>',
        '<script>setTimeout(function () { throw new Error("boom: the board broke"); }, 600);</script></head>',
      ),
    );
  }
};

const boardCard = (page: Page, frame: string) => page.frameLocator(frame).locator('.error:not([hidden])');

/** The card's button sends the error: the project is being edited, the chat is open and holds the message and an answer. */
const sendFromView = async (page: Page, frame: string) => {
  const card = boardCard(page, frame);
  await expect(card).toBeVisible({ timeout: 60_000 });
  await expect(card.getByRole('button', { name: 'Отправить в чат' })).toBeVisible({ timeout: 20_000 });
  expect(page.url()).not.toMatch(/edit=1/);
  await card.getByRole('button', { name: 'Отправить в чат' }).click();
  await expect(page).toHaveURL(/edit=1/, { timeout: 20_000 });
  await expect(
    page.locator('.hx-msg[data-kind="user"]').filter({ hasText: 'boom: the board broke' }).first(),
  ).toBeVisible({
    timeout: 30_000,
  });
  // The fake agent answers with the words it was given.
  await expect(page.getByText(/Fake agent reply/).first()).toBeVisible({ timeout: 120_000 });
  // (The board is drawn again by the switch to editing, so its card may stand with the button anew: the proof is in the chat.)
};

test('a board error goes to the chat from the viewing mode: a site, a deck', async ({ page }) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/designly`);
  await expect(page.locator('.hx-dl-card[data-id="relay-site"], .hx-dl-folder').first()).toBeVisible({
    timeout: 60_000,
  });
  breakBoard('relay-site', 'Главная.dc.html');
  breakBoard('relay-deck', '01 Обложка.dc.html');
  const shots = process.env.TOUR_SHOTS ?? 'test-results';

  await page.goto(`${rt.url}${base}/designly/relay-site`);
  await sendFromView(page, '.hx-site__iframe');
  await page.screenshot({ path: join(shots, 'board-error-site.png') });

  await page.goto(`${rt.url}${base}/designly/relay-deck`);
  await sendFromView(page, '.hx-deck__frame[data-on="1"]');
  await page.screenshot({ path: join(shots, 'board-error-deck.png') });
});
