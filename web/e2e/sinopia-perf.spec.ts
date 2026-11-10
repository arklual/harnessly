// How fast Sinopia opens a document and answers on its canvas (HRN-808, the criterion of moving Sinopia onto the app
// platform): the same scenario is run against two builds and the numbers are compared. It runs only when asked
// (SINOPIA_PERF=<file to write the numbers to>); RUNTIME_BIN, HARNESSLY_BIN and RUNTIME_WEB_DIR name the build.
//
// The times are taken in the pages themselves, not by Playwright's polling: a script in every frame notes the moment
// (performance.timeOrigin + now, one clock for the page and the slides' frames) at the next animation frame after the
// thing appears. Measured:
//   open   — from the navigation's start to the editor, to the first slide's heading, to the app's panel (deck-lint);
//   slide  — from the press on a slide in the strip to its heading on the stage;
//   select — from the press on a block of the slide to the app's selection bar.
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Frame, type Page, test } from '@playwright/test';

import { agreeApp, makeSlides, SLIDES } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';

const OUT = process.env.SINOPIA_PERF ?? '';
const OPENS = Number(process.env.SINOPIA_PERF_OPENS ?? 12);
const ROUNDS = Number(process.env.SINOPIA_PERF_ROUNDS ?? 20);
const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';

test.describe.configure({ mode: 'serial' });
test.skip(!OUT, 'a measurement, run with SINOPIA_PERF=<out.json>');
test.setTimeout(600_000);

/** Notes, in every frame, when the things measured appear and when the person pressed. */
const PROBE = () => {
  const w = window as unknown as { __pf: [string, number][] };
  w.__pf = [];
  const at = () => performance.timeOrigin + performance.now();
  const mark = (k: string) => requestAnimationFrame(() => w.__pf.push([k, at()]));
  addEventListener('pointerdown', () => w.__pf.push(['down', at()]), true);
  let h1 = '';
  let deck = false;
  let panel = false;
  let bar = false;
  const look = () => {
    const h = document.querySelector('h1')?.textContent ?? '';
    if (h !== h1) {
      h1 = h;
      if (h) mark(`h1:${h}`);
    }
    if (!deck && document.querySelector('.hx-deck')) {
      deck = true;
      mark('deck');
    }
    if (!panel && document.querySelector('.hx-sinopia__panel iframe')) {
      panel = true;
      mark('panel');
    }
    const b = document.querySelector('.hx-sinopia__bar') !== null;
    if (b !== bar) {
      bar = b;
      mark(b ? 'bar:1' : 'bar:0');
    }
  };
  new MutationObserver(look).observe(document, { subtree: true, childList: true, characterData: true });
};

let rt: Runtime;
let page: Page;
let project = '';
const result: Record<string, number[]> = {
  deck: [],
  heading: [],
  panel: [],
  slide: [],
  select: [],
  selectRetries: [],
};

const marks = async (p: Page): Promise<[string, number][]> => {
  const out: [string, number][] = [];
  for (const f of p.frames()) {
    const got = await f
      .evaluate(() => (window as unknown as { __pf?: [string, number][] }).__pf ?? [])
      .catch(() => [] as [string, number][]);
    out.push(...got);
  }
  return out.sort((a, b) => a[1] - b[1]);
};

const clear = async (p: Page) => {
  for (const f of p.frames())
    await f.evaluate(() => ((window as unknown as { __pf?: unknown[] }).__pf = [])).catch(() => undefined);
};

const slideFrame = async (p: Page): Promise<Frame> => {
  const handle = await p.locator('iframe.hx-deck__frame').last().elementHandle({ timeout: 30_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error('no frame in the slide');
  return frame;
};

/**
 * The stage shows a slide's heading. The frame is looked for anew on every try: while a slide is changed, the frame of
 * the one before may still be there and go away under a locator held to it.
 */
const shows = (p: Page, head: string) =>
  expect
    .poll(
      async () => {
        const handle = await p
          .locator('iframe.hx-deck__frame')
          .last()
          .elementHandle({ timeout: 2_000 })
          .catch(() => null);
        const frame = await handle?.contentFrame();
        return (
          (await frame
            ?.locator('h1')
            .textContent({ timeout: 2_000 })
            .catch(() => null)) ?? null
        );
      },
      { timeout: 30_000 },
    )
    .toBe(head);

/** The first mark of a key after a moment, or NaN. */
const after = (list: [string, number][], key: string, from: number) =>
  list.find(([k, t]) => k === key && t >= from)?.[1] ?? Number.NaN;

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-perf-'));
  rt = await startRuntime({
    root,
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
    },
  });
  let cli = process.env.HARNESSLY_BIN;
  if (!cli) {
    cli = join(root, 'harnessly');
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  }
  // An app of the document is on, so the editor's side of the apps is in what is measured.
  execFileSync(cli, ['app', 'install', join(repo, 'docs', 'apps', 'deck-lint')], {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
    stdio: 'inherit',
  });
  page = await browser.newPage();
  await page.addInitScript(PROBE);
  // A slow machine, for chasing a race: SINOPIA_PERF_CPU=<rate> slows the page's own process down that many times.
  const cpu = Number(process.env.SINOPIA_PERF_CPU ?? 0);
  if (cpu > 1)
    await (await page.context().newCDPSession(page)).send('Emulation.setCPUThrottlingRate', { rate: cpu });
  // A loaded runtime: SINOPIA_PERF_LAG=<ms> holds every answer of the editor's state up to that long, so they may cross.
  const lag = Number(process.env.SINOPIA_PERF_LAG ?? 0);
  if (lag > 0)
    await page.route('**/apps/sinopia/state**', async (route) => {
      const res = await route.fetch();
      await new Promise((r) => setTimeout(r, Math.random() * lag));
      await route.fulfill({ response: res });
    });
});

test.afterAll(async () => {
  if (OUT) writeFileSync(OUT, JSON.stringify(result, null, 2));
  await page?.close();
  await rt?.stop();
});

test('open: the editor, the first heading and the app’s panel', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  await agreeApp(page, 'harnessly.deck-lint');
  const made = await page.evaluate(async () => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'Perf deck',
        brief: 'Five slides',
        kind: 'slides',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return (await res.json()) as { id?: string; conv?: string };
  });
  project = made.id ?? '';
  const conv = made.conv ?? '';
  expect(conv).toMatch(/^tab-/);
  expect(project).not.toBe('');
  await makeSlides(rt, project, SLIDES);
  // Opened from its chat, as the person comes to a deck: the chat's pane edits, so the apps' panel and bar are there
  // (opened any other way the deck is only looked at).
  const url = `${rt.url}/agents/designly/${project}?chat=${conv}`;
  // The first open warms the caches and shows the release notes once; it is not counted.
  for (let i = 0; i <= OPENS; i++) {
    await page.goto(url);
    await expect(page.locator('.hx-sinopia__panel iframe')).toBeVisible({ timeout: 30_000 });
    await shows(page, SLIDES[0].head);
    // The release notes come a moment after the deck and cover the strip; they show once, on the first open.
    const news = page.getByRole('button', { name: 'Got it' });
    if (i === 0) await news.click({ timeout: 10_000 }).catch(() => undefined);
    else if (await news.isVisible()) await news.click();
    const origin = await page.evaluate(() => performance.timeOrigin);
    const list = await marks(page);
    if (i === 0) continue;
    result.deck.push(after(list, 'deck', origin) - origin);
    result.heading.push(after(list, `h1:${SLIDES[0].head}`, origin) - origin);
    result.panel.push(after(list, 'panel', origin) - origin);
  }
});

test('slide: a slide pressed in the strip shows on the stage', async () => {
  for (let r = 0; r < ROUNDS; r++) {
    const i = 1 + (r % (SLIDES.length - 1));
    await clear(page);
    await page.locator('.hx-deck-th').nth(i).click();
    await shows(page, SLIDES[i].head);
    await page.waitForTimeout(50);
    const list = await marks(page);
    const down = after(list, 'down', 0);
    result.slide.push(after(list, `h1:${SLIDES[i].head}`, down) - down);
    // Back to the first slide, not counted.
    await page.locator('.hx-deck-th').nth(0).click();
    await shows(page, SLIDES[0].head);
  }
});

test('select: a block pressed on the slide brings the app’s bar', async () => {
  for (let r = 0; r < ROUNDS; r++) {
    const frame = await slideFrame(page);
    await clear(page);
    // On a loaded machine a press now and then brings no bar (seen in 3 runs of 6 at load 18; not caught locally with
    // the page's CPU slowed 8 times, nor with the runtime's answers held up to 1.5 s). The press is made again then,
    // counted in `selectRetries`, and the time is taken from the press that brought the bar, so a slow answer still
    // shows in the numbers and a lost press does not stop the measurement.
    let tries = 0;
    for (;;) {
      await frame.locator('p').click();
      const shown = await expect(page.locator('.hx-sinopia__bar'))
        .toBeVisible({ timeout: 5_000 })
        .then(
          () => true,
          () => false,
        );
      if (shown) break;
      tries += 1;
      if (tries > 3) throw new Error('the bar did not come after four presses');
    }
    result.selectRetries.push(tries);
    await page.waitForTimeout(50);
    const list = await marks(page);
    const downs = list.filter(([k]) => k === 'down');
    const down = downs.length > 0 ? downs[downs.length - 1][1] : Number.NaN;
    result.select.push(after(list, 'bar:1', down) - down);
    await frame.locator('main').click({ position: { x: 1800, y: 1000 } });
    await expect(page.locator('.hx-sinopia__bar')).toHaveCount(0, { timeout: 10_000 });
  }
});
