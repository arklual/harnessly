// The caret stays where the person types in a chat's field: at the start of the words, with and without a
// picture attached, with pauses between the keys long enough for the draft to go to the machine and come back.
import { expect, type Page, test } from '@playwright/test';

import { RELEASES } from '../app/next/features/whats-new/releases';
import { type Runtime, startRuntime } from './runtime';

const PASSWORD = 'correct horse';
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
  page = await (await browser.newContext()).newPage();
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await page.locator('.hx-composer__input').fill('hello agent');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: hello agent')).toBeVisible({
    timeout: 60_000,
  });
});

test.afterAll(async () => {
  await page?.context().close();
  await rt?.stop();
});

const field = () => page.locator('.hx-stage .hx-composer__input');

/** Types at the caret key by key with the given pauses; returns where the caret stood after each key. */
async function typeAtStart(pauses: number[]) {
  const input = field();
  await input.fill('проверь логи деплоя');
  // The draft of the fill goes out and comes back before the caret moves.
  await page.waitForTimeout(2500);
  await input.click();
  await input.press('Home');
  await input.press('Control+Home');
  const carets: { value: string; caret: number }[] = [];
  const word = 'Срочно';
  for (let i = 0; i < pauses.length; i += 1) {
    await page.keyboard.type(word[i % word.length]!);
    await page.waitForTimeout(pauses[i]!);
    carets.push(
      await input.evaluate((el: HTMLTextAreaElement) => ({ value: el.value, caret: el.selectionStart })),
    );
  }
  return carets;
}

const PAUSES = [0, 30, 120, 400, 750, 1200, 2100, 3000, 5000];
const expected = PAUSES.map((_, i) => ({
  value: `${'Срочно'.repeat(2).slice(0, i + 1)}проверь логи деплоя`,
  caret: i + 1,
}));

test('the caret stays where the person types at the start of the words', async () => {
  expect(await typeAtStart(PAUSES)).toEqual(expected);
});

test('the caret stays there while the agent works on a long task', async () => {
  let drafts = 0;
  page.on('request', (r) => {
    if ((r.postData() ?? '').includes('draft.set')) drafts += 1;
  });
  await field().fill('a long task please');
  await field().press('Enter');
  await expect(page.locator('.hx-stage .hx-composer [aria-label="Stop the agent"]')).toBeVisible();
  expect(await typeAtStart(PAUSES)).toEqual(expected);
  // The draft did go to the machine meanwhile: the pauses were long enough for its round trips.
  expect(drafts).toBeGreaterThan(1);
});

test('the caret stays there while the same chat is open in another tab', async () => {
  const other = await page.context().newPage();
  await other.goto(page.url());
  await expect(other.locator('.hx-stage .hx-composer__input')).toBeVisible();
  await page.bringToFront();
  try {
    expect(await typeAtStart(PAUSES)).toEqual(expected);
  } finally {
    await other.close();
  }
});

test('the caret stays there with a picture attached to the field', async () => {
  await field().evaluate(
    (el, bytes) => {
      const data = new DataTransfer();
      data.items.add(new File([new Uint8Array(bytes)], 'screen.png', { type: 'image/png' }));
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
    },
    [...PNG],
  );
  await expect(page.locator('.hx-stage .hx-composer .hx-attach')).toHaveCount(1);
  await expect(page.locator('.hx-stage .hx-composer .hx-attach__err')).toHaveCount(0);
  expect(await typeAtStart(PAUSES)).toEqual(expected);
});
