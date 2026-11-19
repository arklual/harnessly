import { execFileSync } from 'node:child_process';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

// how long the right column takes to show its widgets when a chat opens (a real page, a real runtime, N widgets
// made with the real CLI). «cards»: the N cards are in the page; «first»: each shows something (its last content kept in
// the browser, or its live frame); «ready»: every frame is up. The first open has nothing kept; the next ones have it.
// WIDGETS_N sets N (6), WIDGETS_RUNS the repeats (5); the numbers go to the line `widgets-open …`.
const N = Number(process.env.WIDGETS_N ?? 6);
const RUNS = Number(process.env.WIDGETS_RUNS ?? 5);

type Marks = Record<string, number>;

let rt: Runtime;
let page: Page;

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime();
  page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
  page.on('console', (m) => {
    if ((m.type() === 'error' || m.text().startsWith('FRAMEKEY')) && process.env.WIDGETS_DEBUG)
      console.log('CONSOLE', m.text().slice(0, 1500));
  });
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  await page.addInitScript((n: number) => {
    const m: Record<string, number> = {};
    (window as unknown as { __wg: Record<string, number> }).__wg = m;
    const READY = '.hx-appview__skeleton[data-state="ready"]';
    const look = () => {
      const cards = [...document.querySelectorAll('.hx-wg')];
      if (cards.length >= n && m.cards === undefined) m.cards = performance.now();
      const shown = cards.filter((c) => c.querySelector('.hx-wga__kept') || c.querySelector(READY)).length;
      if (shown >= n && m.first === undefined) m.first = performance.now();
      const ready = cards.filter((c) => c.querySelector(READY)).length;
      if (ready >= n && m.ready === undefined) m.ready = performance.now();
    };
    new MutationObserver(look).observe(document, { subtree: true, childList: true, attributes: true });
  }, N);
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill('correct horse');
  await setup.locator('input[name=confirm]').fill('correct horse');
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

const median = (xs: number[]) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)] ?? NaN;

test('the right column shows its widgets at once from what the browser kept', async () => {
  test.setTimeout(300_000);
  const input = page.locator('.hx-composer__input');
  await input.fill('hello');
  await input.press('Enter');
  await expect(page.locator('.hx-feed').getByText('Fake agent reply: hello')).toBeVisible({
    timeout: 60_000,
  });
  const chat = page.url();

  // The CLI of the test's own runtime: nothing of the machine the test runs on (its HARNESSLY_* point at its runtime).
  const env: NodeJS.ProcessEnv = {
    ...Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith('HARNESSLY_'))),
    HOME: rt.home,
    HARNESSLY_DATA_DIR: join(rt.home, '.harnessly'),
    AGENT_DATA_DIR: join(rt.home, '.harnessly'),
    PATH: `${join(rt.home, 'bin')}:${process.env.PATH}`,
  };
  for (let i = 1; i <= N; i++) {
    execFileSync(
      join(rt.home, 'bin', 'harnessly'),
      [
        'widget',
        'create',
        `w${i}`,
        '--title',
        `Widget ${i}`,
        '--scope',
        'global',
        '--html',
        `<p>Widget ${i}: ${'x'.repeat(200)}</p>`,
      ],
      { env, stdio: 'pipe' },
    );
  }

  const open = async (): Promise<Marks> => {
    await page.goto('about:blank');
    await page.goto(chat, { waitUntil: 'commit' });
    try {
      await page.waitForFunction(
        () => (window as unknown as { __wg?: Marks }).__wg?.ready !== undefined,
        null,
        { timeout: 60_000 },
      );
    } catch {
      await page.screenshot({ path: '/tmp/claude-996/widgets-open-fail.png' });
      const state = await page.evaluate(() => ({
        cards: document.querySelectorAll('.hx-wg').length,
        wrap: !!document.querySelector('.hx-wgs-wrap'),
        url: location.href,
        wg: (window as unknown as { __wg: unknown }).__wg,
      }));
      throw new Error(`no widgets: ${JSON.stringify(state)}`);
    }
    return page.evaluate(() => ({ ...(window as unknown as { __wg: Marks }).__wg }));
  };
  // The first open of all: the widgets join the person's stack, and the browser keeps what it reads.
  await open();

  const cold: Record<string, number[]> = { cards: [], first: [], ready: [] };
  const warm: Record<string, number[]> = { cards: [], first: [], ready: [] };
  for (let i = 0; i < RUNS; i++) {
    // Nothing kept: what a person has who never opened a chat with widgets on this browser.
    await page.evaluate(() => {
      for (const k of Object.keys(localStorage))
        if (k.startsWith('hx-wg-apps:') || k.startsWith('hx-wg-content:')) localStorage.removeItem(k);
    });
    const c = await open();
    for (const k of Object.keys(cold)) cold[k]!.push(c[k] ?? NaN);
    const w = await open();
    for (const k of Object.keys(warm)) warm[k]!.push(w[k] ?? NaN);
  }
  const row = (m: Record<string, number[]>) =>
    Object.entries(m)
      .map(([k, v]) => `${k} ${Math.round(median(v))} ms`)
      .join(', ');
  const line = `widgets-open N=${N} runs=${RUNS}: nothing kept: ${row(cold)}; kept: ${row(warm)}`;
  console.log(line);
  test.info().annotations.push({ type: 'widgets-open', description: line });
  expect(median(warm.first!)).toBeLessThanOrEqual(median(cold.first!));
});

test('an update of a widget changes it where it stands: the same frame, no new one', async () => {
  const hl = join(rt.home, 'bin', 'harnessly');
  const env: NodeJS.ProcessEnv = {
    ...Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith('HARNESSLY_'))),
    HOME: rt.home,
    HARNESSLY_DATA_DIR: join(rt.home, '.harnessly'),
    AGENT_DATA_DIR: join(rt.home, '.harnessly'),
  };
  const run = (...args: string[]) => execFileSync(hl, args, { env, stdio: 'pipe' });
  run(
    'widget',
    'create',
    'live',
    '--title',
    'Live',
    '--scope',
    'global',
    '--html',
    '<p id="n">progress 10%</p>',
  );
  await page.goto('about:blank');
  await page.goto(page.url().includes('/c/') ? page.url() : `${rt.url}/`);
  const frame = page.frameLocator('.hx-wg:has-text("Live") iframe.hx-appview');
  await expect(frame.getByText('progress 10%')).toBeVisible({ timeout: 30_000 });
  await page.evaluate(() => {
    const el = [...document.querySelectorAll('.hx-wg')]
      .find((c) => c.textContent?.includes('Live'))
      ?.querySelector('iframe.hx-appview');
    (el as unknown as { __kept: number }).__kept = 1;
  });
  for (const pct of [40, 70, 100]) {
    run('widget', 'update', 'live', '--html', `<p id="n">progress ${pct}%</p>`);
    await expect(frame.getByText(`progress ${pct}%`)).toBeVisible({ timeout: 15_000 });
  }
  const kept = await page.evaluate(() => {
    const el = [...document.querySelectorAll('.hx-wg')]
      .find((c) => c.textContent?.includes('Live'))
      ?.querySelector('iframe.hx-appview');
    return (el as unknown as { __kept?: number } | null)?.__kept ?? 0;
  });
  if (kept !== 1) {
    const info = await page.evaluate(() =>
      [...document.querySelectorAll('.hx-wg')]
        .filter((c) => c.textContent?.includes('Live'))
        .map((c) => c.innerHTML.slice(0, 600)),
    );
    console.log('CARD', JSON.stringify(info));
  }
  expect(kept).toBe(1);
});

test('eight widgets at once: every card shows its content, no update-depth error, no storm in the console', async () => {
  test.setTimeout(180_000);
  const tall = page;
  await tall.setViewportSize({ width: 1920, height: 2600 });
  const errors: string[] = [];
  tall.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text());
  });
  tall.on('pageerror', (e) => errors.push(e.message));
  const env: NodeJS.ProcessEnv = {
    ...Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith('HARNESSLY_'))),
    HOME: rt.home,
    HARNESSLY_DATA_DIR: join(rt.home, '.harnessly'),
    AGENT_DATA_DIR: join(rt.home, '.harnessly'),
  };
  for (let i = 1; i <= 8; i++) {
    execFileSync(
      join(rt.home, 'bin', 'harnessly'),
      [
        'widget',
        'create',
        `eight${i}`,
        '--title',
        `Eight ${i}`,
        '--scope',
        'global',
        '--html',
        `<p>card ${i} is up</p>`,
      ],
      { env, stdio: 'pipe' },
    );
  }
  await tall.reload();
  for (let i = 1; i <= 8; i++) {
    await expect(
      tall.frameLocator(`.hx-wg:has-text("Eight ${i}") iframe.hx-appview`).getByText(`card ${i} is up`),
    ).toBeVisible({ timeout: 40_000 });
  }
  // Let a storm show itself, if there is one.
  await tall.waitForTimeout(4_000);
  expect(await tall.locator('.hx-appfail').count()).toBe(0);
  expect(errors.filter((e) => /Maximum update depth|#185/.test(e))).toEqual([]);
});
