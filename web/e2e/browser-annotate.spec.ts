/// <reference lib="dom" />
import { createServer, type Server } from 'node:http';
import { mkdirSync } from 'node:fs';
import { expect, test, type FrameLocator, type Page } from '@playwright/test';
import { freePort, startRuntime, type Runtime } from './runtime';
import { RELEASES } from '../app/next/features/whats-new/releases';

// «Annotate the page» of the pane's browser tab on localhost pages of every kind: a plain page, a strict
// CSP, shadow DOM, a same-origin iframe, a single-page app that moves with pushState, and a page that forbids framing.
// What is checked is the whole way: the button turns on, a hover shows the element, a click puts a mark with a comment
// field, and «Send» lands the words (address, selector, text, the page's data mark) in the chat's field.

const PASSWORD = 'correct horse';
const shots = process.env.E2E_SCREENSHOTS;

let rt: Runtime;
let page: Page;
let site: Server;
let port: number;

const doc = (body: string, head = '') =>
  `<!doctype html><html><head><meta charset="utf-8"><title>Annotate fixture</title>${head}
<style>body{font:18px system-ui;margin:32px;background:#f4f7fc;color:#16273c}button{font:inherit;padding:10px 18px}
article{max-width:640px;padding:24px;background:#fff;border-radius:16px}iframe{width:100%;height:140px;border:1px solid #ccd}</style></head>
<body>${body}</body></html>`;

const PAGES: Record<string, { headers?: Record<string, string>; html: string }> = {
  '/plain': {
    html: doc(
      `<article><h1>Plain page</h1><p id="words">Some words to annotate</p><button id="buy">Buy now</button></article>`,
    ),
  },
  '/csp': {
    headers: {
      'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'",
    },
    html: doc(
      `<article><h1>Strict CSP</h1><p id="words">Some words to annotate</p><button id="buy">Buy now</button></article>`,
    ),
  },
  '/shadow': {
    html: doc(
      `<article><h1>Shadow DOM</h1><div id="host"></div></article>
<script>const r=document.getElementById('host').attachShadow({mode:'open'});r.innerHTML='<p id="words">Words inside a shadow root</p><button id="buy">Buy now</button>'</script>`,
    ),
  },
  '/frames': {
    html: doc(
      `<article><h1>Frames</h1><p id="words">Outer words</p><iframe id="inner" src="/inner"></iframe></article>`,
    ),
  },
  '/inner': { html: doc(`<p id="words">Words in an inner frame</p><button id="buy">Inner button</button>`) },
  '/spa': {
    html: doc(
      `<article><h1>SPA</h1><p id="words">First route words</p><button id="go">Go on</button></article>
<script>document.getElementById('go').onclick=()=>{history.pushState({},'','/spa/next');
document.querySelector('article').innerHTML='<h1>Second route</h1><p id="words">Second route words</p><button id="buy">Buy now</button>'}</script>`,
    ),
  },
  '/deny': {
    headers: { 'X-Frame-Options': 'DENY', 'Content-Security-Policy': "frame-ancestors 'none'" },
    html: doc(`<article><h1>Denies framing</h1><p id="words">Some words to annotate</p></article>`),
  },
};

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats,design' } });
  port = await freePort();
  site = createServer((req, res) => {
    const path = new URL(req.url ?? '/', 'http://x').pathname;
    if (path === '/notes.txt') {
      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.end('just text, no page');
      return;
    }
    const found = PAGES[path] ?? (path.startsWith('/spa/') ? PAGES['/spa'] : undefined);
    if (!found) {
      res.statusCode = 404;
      res.end('not found');
      return;
    }
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    for (const [k, v] of Object.entries(found.headers ?? {})) res.setHeader(k, v);
    res.end(found.html);
  });
  await new Promise<void>((ok) => site.listen(port, '127.0.0.1', ok));
  if (shots) mkdirSync(shots, { recursive: true });
  page = await browser.newPage();
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
  await page.locator('.hx-composer__input').fill('hello agent');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: hello agent')).toBeVisible({
    timeout: 60_000,
  });
  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  await expect(pane).toBeVisible();
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="globe"]') })
    .click();
  await expect(page.locator('.hx-tbrowser')).toBeVisible();
});

test.afterAll(async () => {
  await page?.close();
  await new Promise((ok) => site?.close(ok));
  await rt?.stop();
});

const pane = () => page;
const frame = (): FrameLocator => pane().frameLocator('.hx-tbrowser__frame');
const pickButton = () => pane().getByRole('button', { name: 'Annotate the page' });

const open = async (path: string) => {
  await pane().locator('.hx-tbrowser__url').fill(`localhost:${port}${path}`);
  await pane().locator('.hx-tbrowser__url').press('Enter');
  await expect(frame().locator('h1').first()).toBeVisible();
};

/** Turns annotating on, puts one mark on `target` with a comment, sends it, and returns the chat's field. */
const annotate = async (name: string, target: () => ReturnType<FrameLocator['locator']>, comment: string) => {
  await expect(pickButton()).not.toHaveAttribute('aria-disabled', 'true', { timeout: 15_000 });
  await pickButton().click();
  await expect(pickButton()).toHaveAttribute('aria-pressed', 'true');
  await target().hover();
  await target().click();
  const field = pane().getByPlaceholder('What to do with this?');
  await expect(field).toBeVisible();
  if (shots) await page.screenshot({ path: `${shots}/${name}-comment.png` });
  await field.fill(comment);
  await field.press('Enter');
  await pane().locator('.hx-note__send').click();
  // The marks wait in the chat's field as chips; the words go to the agent with the message.
  const draft = page.locator('.hx-composer__input');
  await draft.fill(`see ${comment}`);
  await draft.press('Enter');
  await expect(page.locator('.hx-feed')).toContainText(`Comment: ${comment}`, { timeout: 30_000 });
  return page.locator('.hx-feed');
};

for (const [name, path, pick] of [
  ['plain', '/plain', '#words'],
  ['csp', '/csp', '#words'],
  ['shadow', '/shadow', '#words'],
] as const) {
  test(`annotate works on a ${name} localhost page and the words reach the chat`, async () => {
    await open(path);
    const feed = await annotate(name, () => frame().locator(pick), `fix ${name}`);
    const text = await feed.innerText();
    expect(text).toContain(`localhost:${port}${path}`);
    expect(text).toContain('words');
  });
}

test('annotate works inside a same-origin iframe', async () => {
  await open('/frames');
  const inner = pane().frameLocator('.hx-tbrowser__frame').frameLocator('#inner');
  const draft = await annotate('frames', () => inner.locator('#words'), 'fix inner');
  expect(await draft.innerText()).toContain('inner frame');
});

test('annotate stays available after the page moves by pushState', async () => {
  await open('/spa');
  await frame().locator('#go').click();
  await expect(frame().getByRole('heading', { name: 'Second route' })).toBeVisible();
  const draft = await annotate('spa', () => frame().locator('#words'), 'fix route');
  expect(await draft.innerText()).toContain('Second route');
});

test('a page that forbids framing is still annotated: the proxy lifts the ban for the pane', async () => {
  await open('/deny');
  const feed = await annotate('deny', () => frame().locator('#words'), 'fix deny');
  expect(await feed.innerText()).toContain('/deny');
});

test('a page without the bridge says why next to the button, with a retry', async () => {
  await pane().locator('.hx-tbrowser__url').fill(`localhost:${port}/notes.txt`);
  await pane().locator('.hx-tbrowser__url').press('Enter');
  await expect(pickButton()).toBeVisible();
  await pickButton().click({ force: true });
  const note = pane().getByRole('alert');
  await expect(note).toBeVisible({ timeout: 10_000 });
  await expect(note.getByRole('button', { name: 'Try again' })).toBeVisible();
  if (shots) await page.screenshot({ path: `${shots}/no-bridge.png` });
  await note.getByRole('button', { name: 'Try again' }).click();
  await expect(note).toBeVisible({ timeout: 10_000 });
});
