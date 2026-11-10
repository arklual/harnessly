// How «Сделано в этом чате» loads on a phone over a slow network (HRN-510): the tools home is opened cold and again
// warm with several documents, boards and pictures, and the layout shifts (CLS) and what is on screen at 0.3 s, 1 s and
// 3 s are recorded. Titles come late and one by one on purpose (each link answers after its own pause).
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect as baseExpect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const PASSWORD = 'correct horse';
const expect = baseExpect.configure({ timeout: 120_000 });
const shots = () => {
  const dir = process.env.E2E_SHOTS ?? tmpdir();
  mkdirSync(dir, { recursive: true });
  return dir;
};
/** Lighthouse's «slow 4G»: 150 ms of round trip, 1.6 Mbit/s down, 750 kbit/s up. */
const SLOW_4G = {
  offline: false,
  latency: 150,
  downloadThroughput: (1.6 * 1024 * 1024) / 8,
  uploadThroughput: (750 * 1024) / 8,
};
/**
 * CLS at most this for a cold open (a layout shift score). One shift stays: the groups below move once when the
 * number of things made becomes known (3 slots become 7); names and pictures fill fixed slots and do not shift.
 */
const CLS_BUDGET = 0.15;

test.describe.configure({ mode: 'serial', timeout: 900_000 });

let rt: Runtime;
let page: Page;
let convUrl = '';
let state = '';
const PICTURES = 4;

const DOCS = Array.from({ length: 5 }, (_, i) => ({
  slug: `doc-${i + 1}`,
  title: `The document number ${i + 1} of this chat, with a longer name`,
}));
const MADE = [
  ...DOCS.map((d, i) => ({ ref: `https://ai.example.test/agents/docs/${d.slug}`, kind: 'doc', at: 100 - i })),
  { ref: 'https://b.example.test/b/board-one?k=KEYONE', kind: 'board', at: 50 },
  { ref: 'https://b.example.test/b/board-two?k=KEYTWO', kind: 'board', at: 40 },
].map((x) => ({ ...x, title: '', op: 'changed', cover: null, designKind: null }));

/** Layout shifts of the page since the script started, kept on window. */
const OBSERVE = `
  window.__cls = { total: 0, entries: [] };
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) {
      if (e.hadRecentInput) continue;
      window.__cls.total += e.value;
      window.__cls.entries.push({ at: Math.round(e.startTime), value: Number(e.value.toFixed(4)),
        nodes: (e.sources || []).map((s) => (s.node && (s.node.className || s.node.nodeName)) + ' ' + Math.round(s.previousRect.y) + '>' + Math.round(s.currentRect.y) + ' h' + Math.round(s.previousRect.height) + '>' + Math.round(s.currentRect.height)).slice(0, 4) });
    }
  }).observe({ type: 'layout-shift', buffered: true });
`;

const stub = async (p: Page) => {
  await p.route(/\/conversations\/[^/]+\/made(\?|$)/, async (route) => {
    await new Promise((r) => setTimeout(r, 400));
    await route.fulfill({ json: { items: MADE, complete: true } });
  });
  await p.route(/\/link-meta\?/, async (route) => {
    const url = new URL(route.request().url()).searchParams.get('url') ?? '';
    const at = [...DOCS.map((d) => d.slug), 'board-one', 'board-two'].findIndex((s) => url.includes(`/${s}`));
    // Each answers after a pause of its own, as a loaded runtime on a weak network does.
    await new Promise((r) => setTimeout(r, 300 + at * 350));
    const doc = DOCS[at];
    await route.fulfill({
      json: { kind: 'artifact', title: doc ? doc.title : `Board ${at - DOCS.length + 1}`, description: null },
    });
  });
};

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-load-e2e-'));
  rt = await startRuntime({ root, env: { RUNTIME_WEB_FEATURES: 'chats,design' } });
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    colorScheme: 'dark',
  });
  page = await ctx.newPage();
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  await page.addLocatorHandler(
    page.locator('.hx-notices .hx-notice__x').first(),
    async (x) => {
      await x.dispatchEvent('click', undefined, { timeout: 2000 }).catch(() => undefined);
    },
    { noWaitAfter: true },
  );
});

test.afterAll(async () => {
  await page?.context().close();
  await rt?.stop();
});

test('1. a chat that names some pictures', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  // Pictures of a real size, made by the page itself: the message names their paths.
  const names: string[] = [];
  for (let i = 0; i < PICTURES; i += 1) {
    const file = join(rt.home, 'project', `look-${i + 1}.png`);
    writeFileSync(file, await page.screenshot({ fullPage: true }));
    names.push(file);
  }
  await page.locator('.hx-composer__input').fill(`Screens: ${names.join(' ')}`);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  convUrl = page.url();
  await expect(page.getByText('Fake agent reply:')).toBeVisible();
  state = JSON.stringify(await page.context().storageState());
  writeFileSync(join(shots(), 'state.json'), state);
});

const measure = async (
  browser: import('@playwright/test').Browser,
  tag: string,
  keep?: Page,
  reload = false,
) => {
  let p = keep;
  if (p && reload) {
    await p.reload();
    await expect(p.getByText('Fake agent reply:')).toBeVisible();
    await p.waitForTimeout(500);
  }
  let ctx: import('@playwright/test').BrowserContext | null = null;
  if (!p) {
    ctx = await browser.newContext({
      storageState: JSON.parse(state),
      viewport: { width: 390, height: 844 },
      hasTouch: true,
      isMobile: true,
      colorScheme: 'dark',
    });
    p = await ctx.newPage();
    await p.addInitScript(OBSERVE);
    await p.addLocatorHandler(p.getByRole('button', { name: 'Got it' }), (b) => b.click());
    await p.addLocatorHandler(
      p.locator('.hx-notices .hx-notice__x').first(),
      async (x) => {
        await x.dispatchEvent('click', undefined, { timeout: 2000 }).catch(() => undefined);
      },
      { noWaitAfter: true },
    );
    await stub(p);
    await p.goto(convUrl);
    await expect(p.getByText('Fake agent reply:')).toBeVisible();
  }
  const cdp = await p.context().newCDPSession(p);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', SLOW_4G);
  await p.evaluate(() => {
    (window as unknown as { __cls: { total: number; entries: unknown[] } }).__cls = { total: 0, entries: [] };
  });
  const started = Date.now();
  await p.getByRole('button', { name: 'Tools · side chat' }).click();
  const out: Record<string, unknown> = {};
  for (const [label, at] of [
    ['0.3s', 300],
    ['1s', 1000],
    ['3s', 3000],
  ] as const) {
    await p.waitForTimeout(Math.max(0, at - (Date.now() - started)));
    await p.screenshot({ path: join(shots(), `${tag}-${label.replace('.', '_')}.png`) });
    out[label] = await p.evaluate(() => ({
      cards: document.querySelectorAll('.hx-made__card').length,
      named: [...document.querySelectorAll('.hx-made__name')].map((x) => x.textContent).length,
      bars: document.querySelectorAll('.hx-made__name-sk').length,
    }));
  }
  await p.waitForTimeout(6000);
  out.cls = await p.evaluate(() => (window as unknown as { __cls: unknown }).__cls);
  out.requests = state && 0;
  console.log(`MEASURE ${tag} ${JSON.stringify(out)}`);
  await cdp.send('Network.emulateNetworkConditions', {
    offline: false,
    latency: 0,
    downloadThroughput: -1,
    uploadThroughput: -1,
  });
  return { p, ctx, out };
};

test('2. cold open, then warm open, over slow 4G', async ({ browser }) => {
  const cold = await measure(browser, 'cold');
  const cls = (cold.out.cls as { total: number }).total;
  // Open and close the pane again on the same page: the block draws from what it kept.
  await cold.p.locator('.hx-pane .hx-tochat').click();
  await expect(cold.p.locator('.hx-pane')).toHaveCount(0);
  await cold.p.waitForTimeout(600);
  const warm = await measure(browser, 'warm', cold.p);
  await cold.p.locator('.hx-pane .hx-tochat').click();
  await expect(cold.p.locator('.hx-pane')).toHaveCount(0);
  // The page loaded again: only what the browser kept (localStorage) is there.
  const again = await measure(browser, 'reload', cold.p, true);
  console.log(
    `CLS cold=${cls} warm=${(warm.out.cls as { total: number }).total} reload=${(again.out.cls as { total: number }).total} budget=${CLS_BUDGET}`,
  );
  await cold.ctx?.close();
  readFileSync(join(shots(), 'state.json'));
  expect(cls).toBeLessThanOrEqual(CLS_BUDGET);
});
