// How fast a design board loads: node web/scripts/board-load.mjs <url> [--runs 3] [--cookie name=value] [--verbose]
//
// <url> is a board of the design library server (http://127.0.0.1:<port>/a/<project>/p/<board>). Each run opens a
// fresh headless Chromium context and loads the board twice in it: cold (empty cache) and warm (the cache the cold
// load filled). «Seen» is the moment the board's frame, with its nested iframes, holds more than 300 elements.
// Requests and bytes are what the warm load asked the network for (answers from the cache are not counted;
// a revalidation answered 304 is, with its few hundred bytes).
//
// CHROME_BIN names a Chromium to use when the one Playwright expects is not installed.
//
// Per run:  cold <ms> warm <ms> requests <n> bytes <n>
// Then, for the last run, `cold-requests <n> cold-bytes <n>` and a `duplicate <n>x <url>` line for every address the
// cold load fetched more than once (one bundle downloaded twice).
import { chromium } from '@playwright/test';

const SEEN_ELEMENTS = 300;
const TIMEOUT_MS = 60_000;

const args = process.argv.slice(2);
const url = args.find((a) => /^https?:/.test(a));
const flag = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
};
const runs = Number(flag('--runs', '3'));
const cookie = flag('--cookie', '');
const verbose = args.includes('--verbose');
if (!url || !Number.isInteger(runs) || runs < 1) {
  console.error('usage: node web/scripts/board-load.mjs <url> [--runs 3] [--cookie name=value]');
  process.exit(2);
}

/** Elements in the page's frames: the board's frame and every iframe nested in it. */
const elementsIn = async (page) => {
  let total = 0;
  for (const frame of page.frames()) {
    try {
      total += await frame.evaluate(() => document.querySelectorAll('*').length);
    } catch {
      // a frame navigating away: counted on the next tick
    }
  }
  return total;
};

/** One load in a page of the context: milliseconds until seen, and what crossed the network. */
async function load(context) {
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  const requests = new Map();
  const cached = new Set();
  cdp.on('Network.requestWillBeSent', (e) => {
    if (!e.request.url.startsWith('data:'))
      requests.set(e.requestId, { url: e.request.url, bytes: 0, status: 0 });
  });
  cdp.on('Network.requestServedFromCache', (e) => cached.add(e.requestId));
  cdp.on('Network.responseReceived', (e) => {
    const r = requests.get(e.requestId);
    if (!r) return;
    r.status = e.response.status;
    if (e.response.fromDiskCache || e.response.fromPrefetchCache) cached.add(e.requestId);
  });
  cdp.on('Network.loadingFinished', (e) => {
    const r = requests.get(e.requestId);
    if (r) r.bytes = e.encodedDataLength;
  });
  const started = Date.now();
  await page.goto(url, { waitUntil: 'commit', timeout: TIMEOUT_MS });
  let ms = -1;
  while (Date.now() - started < TIMEOUT_MS) {
    if ((await elementsIn(page)) > SEEN_ELEMENTS) {
      ms = Date.now() - started;
      break;
    }
    await page.waitForTimeout(25);
  }
  // The load is over when the network is quiet: the bytes of the late requests count too.
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  await page.close();
  const fetched = [...requests].filter(([id]) => !cached.has(id)).map(([, r]) => r);
  return { ms, fetched };
}

const sum = (list) => list.reduce((n, r) => n + r.bytes, 0);
let last;
for (let run = 0; run < runs; run++) {
  const browser = await chromium.launch({
    headless: true,
    executablePath: process.env.CHROME_BIN || undefined,
  });
  const context = await browser.newContext();
  if (cookie) {
    const [name, ...value] = cookie.split('=');
    await context.addCookies([{ name, value: value.join('='), url }]);
  }
  const cold = await load(context);
  const warm = await load(context);
  await browser.close();
  console.log(`cold ${cold.ms} warm ${warm.ms} requests ${warm.fetched.length} bytes ${sum(warm.fetched)}`);
  last = { cold, warm };
}

if (verbose) {
  for (const [name, load] of [
    ['cold', last.cold],
    ['warm', last.warm],
  ])
    for (const r of load.fetched) console.log(`${name} ${r.status} ${r.bytes} ${decodeURI(r.url)}`);
}
// A bundle downloaded twice is the same address answered with a body twice (a 304 is only a check).
const seen = new Map();
for (const r of last.cold.fetched.filter((r) => r.status === 200))
  seen.set(r.url, (seen.get(r.url) ?? 0) + 1);
console.log(`cold-requests ${last.cold.fetched.length} cold-bytes ${sum(last.cold.fetched)}`);
for (const [u, n] of seen) if (n > 1) console.log(`duplicate ${n}x ${u}`);

// The same bundle under two addresses is two downloads too: bodies of one size (10 KB and up) at different addresses.
const bySize = new Map();
for (const r of last.cold.fetched.filter((r) => r.status === 200 && r.bytes >= 10_000)) {
  bySize.set(r.bytes, [...(bySize.get(r.bytes) ?? []), r.url]);
}
for (const [bytes, urls] of bySize) {
  const distinct = [...new Set(urls)];
  if (distinct.length > 1)
    console.log(
      `same-size ${bytes} B at ${distinct.length} addresses: ${distinct.map(decodeURI).join(' | ')}`,
    );
}
