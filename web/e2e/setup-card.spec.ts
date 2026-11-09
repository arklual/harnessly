/// <reference lib="dom" />
import { expect, type Locator, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

// The start page's «Настройка машины» card takes the person's pointer (GitHub issue 59): the greeting it sits in lets
// clicks through to the composer, and the card used to inherit that, so no button of it answered a real click. Each
// button here is pressed with the mouse at its centre, the way a person does, never with a synthetic click; a card
// that is hidden or fading out lets the composer have the pointer again.
//
// The machine's list is answered by the test, so the card always shows the same thing: a secret it waits for, a
// repository that failed with a long message and a plugin that is done.

const PASSWORD = 'correct horse';

const VIEW = {
  allowed: true,
  hash: 'e2e-setup-59',
  done: false,
  run: null,
  items: [
    {
      itemId: 'access:token',
      kind: 'access',
      name: 'Example token',
      status: 'pending',
      message: '',
      secret: 'example-token',
    },
    {
      itemId: 'repo:demo',
      kind: 'repo',
      name: 'demo-repo',
      status: 'error',
      message:
        'git clone https://git.example.com/demo-repo.git failed: the server answered 403 for this account. '.repeat(
          3,
        ),
      secret: '',
    },
    { itemId: 'plugin:demo', kind: 'plugin', name: 'demo-plugin', status: 'ok', message: '', secret: '' },
  ],
};
const BASE = { service: false, running: false, done: true, proxy: '', command: '', items: [] };

let rt: Runtime;
let page: Page;
let runs = 0;

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
  page = await browser.newPage();
  await page.route(/\/environment\/run(\?.*)?$/, (route) => {
    runs += 1;
    return route.fulfill({ json: { runId: `run-${runs}` } });
  });
  await page.route(/\/environment(\?.*)?$/, (route) => route.fulfill({ json: VIEW }));
  await page.route(/\/basepkg(\?.*)?$/, (route) => route.fulfill({ json: BASE }));
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

/** Presses the mouse at the middle of the element, as a person does: whatever is on top there gets the click. */
const press = async (el: Locator) => {
  await el.scrollIntoViewIfNeeded();
  const box = await el.boundingBox();
  if (!box) throw new Error('the element has no box');
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
};

/** What is on top at the middle of el when it is not el or inside it ('' when it is): whatever takes the click. */
const coveredBy = (el: Locator) =>
  el.evaluate((node) => {
    const r = node.getBoundingClientRect();
    const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    if (top && node.contains(top)) return '';
    return top ? `${top.tagName.toLowerCase()}.${top.className}` : 'nothing';
  });

const card = () => page.locator('.hx-setup');

test('the person signs in and the setup card shows on the home screen', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  await expect(card()).toBeVisible();
  await expect(card()).toHaveAttribute('data-closing', 'false');
  // «Что нового» and the welcome of the lessons come up once on a fresh browser, at the top of the window, over the
  // card; the person puts them away first.
  for (const close of ['.hx-news__ok', '.hx-learn-hello__x']) {
    const el = page.locator(close);
    await el.waitFor({ timeout: 10_000 }).then(
      () => el.click(),
      () => undefined,
    );
    await expect(el).toHaveCount(0);
  }
});

test('a stage, «Подробнее» and «Повторить» answer the mouse', async () => {
  const nodes = card().locator('.hx-chain__node');
  await expect(nodes).toHaveCount(3);
  for (const b of [
    nodes.nth(1),
    card().locator('.hx-setup__more'),
    card().locator('.hx-setup__pill[data-main=true]'),
  ])
    expect(await coveredBy(b)).toBe('');

  await press(nodes.nth(1));
  await expect(nodes.nth(1)).toHaveAttribute('aria-pressed', 'true');
  await expect(card().locator('.hx-inside__it', { hasText: 'demo-repo' })).toBeVisible();

  const more = card().locator('.hx-setup__more');
  await press(more);
  await expect(more).toHaveAttribute('aria-expanded', 'true');

  await press(card().locator('.hx-setup__pill[data-main=true]'));
  await expect.poll(() => runs).toBe(1);
});

test('the step that waits for a secret opens the safe', async () => {
  const nodes = card().locator('.hx-chain__node');
  await press(nodes.nth(0));
  await expect(nodes.nth(0)).toHaveAttribute('aria-pressed', 'true');
  await press(card().locator('.hx-inside__it[data-act=true]', { hasText: 'Example token' }));
  await expect(page.locator('.hx-set')).toBeVisible();
});

test('a card fading out or hidden lets the composer have the pointer', async () => {
  // The safe opened in its own tab, which a reload keeps; closing it brings the home screen back.
  await page
    .getByRole('tab', { name: /Settings|Настройки/ })
    .getByRole('button')
    .click();
  await expect(card()).toBeVisible();
  // While it fades out the card takes no click: what is under it gets it.
  await card().evaluate((el) => el.setAttribute('data-closing', 'true'));
  expect(await coveredBy(card())).not.toBe('');
  await card().evaluate((el) => el.setAttribute('data-closing', 'false'));
  expect(await coveredBy(card())).toBe('');

  await press(card().locator('.hx-setup__quiet'));
  await expect(card()).toHaveCount(0);
  const input = page.locator('.hx-composer__input');
  await press(input);
  await expect(input).toBeFocused();
  await page.keyboard.type('typed after the card');
  await expect(input).toHaveValue('typed after the card');
});
