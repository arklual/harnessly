// How fast a deck turns to the next slide in the real editor: node web/scripts/deck-turn.mjs <runtime url> <project id>
//   [--runs 3] [--edit <slide file>] [--settle 4000] [--tunnel]
//
// <runtime url> is a standalone runtime of its own (see docs/design-pages.md, Speed: never the person's), started with
// RUNTIME_DESIGN_LIBRARY_DIR on a copy of the deck's project; the script sets the runtime's password up when it asks for
// one (and signs in when it is set). Each run opens the editor in a fresh browser context and measures, in the page's own clock, the time from the
// key that turns the slide until the new slide's frame is on screen (`data-top=1 data-on=1`), the board drawn:
//   open       the editor's first slide, from the address to its frame on screen
//   turn       ArrowRight to slide 2 after the slide on screen has rested `--settle` ms (the neighbours load meanwhile)
//   turn-warm  ArrowLeft to slide 1, then ArrowRight to slide 2 again
//   turn-edit  slide 2's file changed (`--edit <file>`: a comment line is appended) while the slide on screen is 1; then
//              ArrowRight
// Prints `open <ms> turn <ms> turn-warm <ms> turn-edit <ms>` per run (-1: not measured) and a `median` line.
import { appendFileSync } from 'node:fs';
import { chromium } from '@playwright/test';

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
};
const [base, project] = args.filter(
  (a, i) => !a.startsWith('--') && !(i > 0 && args[i - 1].startsWith('--')),
);
const runs = Number(flag('--runs', '3'));
const settle = Number(flag('--settle', '4000'));
const editFile = flag('--edit', '');
// --tunnel: the link of a machine behind the hub's tunnel (HRN-994 plan: ~1.3 MB/s, tens of ms to the first byte).
const tunnel = args.includes('--tunnel');
const PASSWORD = 'correct horse';
const TIMEOUT_MS = 60_000;
if (!/^https?:/.test(base ?? '') || !project || !Number.isInteger(runs) || runs < 1) {
  console.error(
    'usage: node web/scripts/deck-turn.mjs <runtime url> <project id> [--runs 3] [--edit <file>]',
  );
  process.exit(2);
}

// The slide on screen: the frame on top (`data-top`; before HRN-994 the last one) once it has faded in.
const TOP = `(() => {
  const all = [...document.querySelectorAll('iframe.hx-deck__frame')];
  const top = all.find((f) => f.dataset.top === '1') ?? all[all.length - 1];
  return top && top.dataset.on === '1' ? top : null;
})()`;

/** Milliseconds from now (the page's clock) until a frame other than `not` is on screen. */
const untilShown = (page, not) =>
  page.evaluate(
    ({ top, not, limit }) =>
      new Promise((done) => {
        const t0 = performance.now();
        const poll = () => {
          const frame = eval(top);
          if (frame && frame.getAttribute('src') !== not) return done(performance.now() - t0);
          if (performance.now() - t0 > limit) return done(-1);
          requestAnimationFrame(poll);
        };
        poll();
      }),
    { top: TOP, not, limit: TIMEOUT_MS },
  );

const srcOnScreen = (page) => page.evaluate((top) => eval(top)?.getAttribute('src') ?? null, TOP);

/** The key is pressed in the page, and the clock starts in the same task. */
const turn = async (page, key) => {
  const was = await srcOnScreen(page);
  const shown = untilShown(page, was);
  await page.keyboard.press(key);
  return Math.round(await shown);
};

const rows = [];
for (let run = 0; run < runs; run++) {
  const browser = await chromium.launch({
    headless: true,
    executablePath: process.env.CHROME_BIN || undefined,
  });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  if (tunnel) {
    const cdp = await context.newCDPSession(page);
    await cdp.send('Network.enable');
    await cdp.send('Network.emulateNetworkConditions', {
      offline: false,
      latency: 40,
      downloadThroughput: 1_300_000,
      uploadThroughput: 1_300_000,
    });
  }
  const started = Date.now();
  await page.goto(`${base}/`);
  const form = page.locator('[data-testid="setup-form"], [data-testid="signin-form"]').first();
  if (
    await form.waitFor({ timeout: 15_000 }).then(
      () => true,
      () => false,
    )
  ) {
    await form.locator('input[name=password]').fill(PASSWORD);
    const confirm = form.locator('input[name=confirm]');
    if (await confirm.count()) await confirm.fill(PASSWORD);
    await form.locator('button[type=submit]').click();
  }
  await page.locator('.hx-app').waitFor({ timeout: TIMEOUT_MS });
  const opened = Date.now();
  await page.goto(`${base}/agents/designly/${project}`);
  await page.waitForFunction((top) => eval(top) !== null, TOP, { timeout: TIMEOUT_MS });
  const open = Date.now() - opened;
  const news = page.getByRole('button', { name: 'Got it' });
  if (await news.isVisible().catch(() => false)) await news.click();
  await page
    .locator('.hx-deck__stage')
    .click({ position: { x: 4, y: 4 } })
    .catch(() => {});
  void started;

  await page.waitForTimeout(settle);
  const cold = await turn(page, 'ArrowRight');
  await page.waitForTimeout(settle);
  await turn(page, 'ArrowLeft');
  await page.waitForTimeout(500);
  const warm = await turn(page, 'ArrowRight');
  let edited = -1;
  if (editFile) {
    await page.waitForTimeout(settle);
    await turn(page, 'ArrowLeft');
    await page.waitForTimeout(settle);
    appendFileSync(editFile, `\n<!-- deck-turn ${Date.now()} -->\n`);
    await page.waitForTimeout(1500);
    edited = await turn(page, 'ArrowRight');
  }
  await browser.close();
  rows.push({ open, cold, warm, edited });
  console.log(`open ${open} turn ${cold} turn-warm ${warm} turn-edit ${edited}`);
}
const median = (key) => {
  const v = rows.map((r) => r[key]).sort((a, b) => a - b);
  return v[Math.floor(v.length / 2)];
};
console.log(
  `median open ${median('open')} turn ${median('cold')} turn-warm ${median('warm')} turn-edit ${median('edited')}`,
);
