#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
//
// Differential run of the board engine: every *.dc.html of a corpus is drawn on two engines, the reference one (the
// support.js beside the board, or --vendor) and ours (internal/design/ownengine/support.js), and the results are compared:
// the normalized DOM (tags, attributes, text), the data-dc-tpl set, console errors and a screenshot, pixel by pixel.
// With --control the reference engine is drawn a second time: a board that differs from itself is noise, not a finding.
// With --interact the tweaks of each board are changed and its controls clicked, and the DOM compared after each step.
//
// No reference engine ships with this repository: the script works only where a corpus holds one. Nothing is written to
// the corpus: the boards are copied into --work, everything else is read through a local server bound to 127.0.0.1.
// The boards' scripts run in the browser and can ask that server for anything, so it answers only under a random token of
// the run, only for the folders of the boards found here, only for files really inside them (symlinks resolved), plus
// the explicit list of the harness's own files (the engines, the runtime's vendor files).
//
//   nice -n 15 node scripts/design-engine-parity.mjs --corpus /path/to/designs --work /tmp/parity [--sample 10]
//     [--filter <regex>] [--control] [--interact] [--pages 2] [--vendor <support.js>] [--engine <support.js>]
//     [--playwright <dir with node_modules>] [--chromium <binary>] [--settle-ms 8000] [--out <file.json>]

import { createHash, randomBytes } from 'node:crypto';
import dns from 'node:dns';
import fs from 'node:fs';
import http from 'node:http';
import https from 'node:https';
import { createRequire } from 'node:module';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import zlib from 'node:zlib';

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function parseArgs(argv) {
  const out = { pages: 2, settleMs: 8000, restartEvery: 40 };
  const names = { '--settle-ms': 'settleMs', '--restart-every': 'restartEvery' };
  for (let i = 0; i < argv.length; i += 1) {
    const flag = argv[i];
    if (flag === '--control' || flag === '--interact') { out[flag.slice(2)] = true; continue; }
    if (!flag.startsWith('--') || i + 1 >= argv.length) throw new Error(`unknown argument ${flag}`);
    out[names[flag] ?? flag.slice(2)] = argv[(i += 1)];
  }
  if (!out.corpus || !out.work) throw new Error('--corpus and --work are required');
  out.pages = Math.min(2, Number(out.pages)); // a live machine: never more than two pages at once
  out.settleMs = Number(out.settleMs);
  out.restartEvery = Number(out.restartEvery);
  out.engine = path.resolve(out.engine ?? path.join(REPO, 'internal/design/ownengine/support.js'));
  return out;
}

function loadPlaywright(args) {
  const places = [args.playwright, path.join(REPO, 'web')].filter(Boolean);
  for (const place of places) {
    const require = createRequire(path.join(path.resolve(place), 'x.js'));
    for (const name of ['playwright-core', 'playwright', '@playwright/test']) {
      try { return require(name); } catch { /* the next one */ }
    }
  }
  throw new Error('playwright not found: install web/ dependencies or pass --playwright <dir>');
}

// The corpus: each board with its project folder. Folders starting with a dot are not projects.
function findBoards(corpus) {
  const boards = [];
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue;
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith('.dc.html')) boards.push({ dir, file: entry.name, id: path.relative(corpus, full) });
    }
  };
  walk(corpus);
  return boards.sort((a, b) => a.id.localeCompare(b.id));
}

// A spread of boards: round robin over projects, each project's boards from the largest down.
function sample(boards, n) {
  const byProject = new Map();
  for (const board of boards) {
    const list = byProject.get(board.dir) ?? [];
    list.push(board);
    byProject.set(board.dir, list);
  }
  for (const list of byProject.values()) {
    list.sort((a, b) => fs.statSync(path.join(b.dir, b.file)).size - fs.statSync(path.join(a.dir, a.file)).size);
  }
  const out = [];
  const lists = [...byProject.values()];
  for (let round = 0; out.length < n && lists.some((l) => l.length > round); round += 1) {
    for (const list of lists) if (list[round] && out.length < n) out.push(list[round]);
  }
  return out;
}

const VENDOR_URLS = {
  'https://unpkg.com/react@18.3.1/umd/react.production.min.js': 'react.production.min.js',
  'https://unpkg.com/react-dom@18.3.1/umd/react-dom.production.min.js': 'react-dom.production.min.js',
  'https://unpkg.com/@babel/standalone@7.29.0/babel.min.js': 'babel.min.js',
};

// The same map and the same getter the runtime puts before the engine (internal/design/vendor.go).
function resourcesScript(token) {
  const map = Object.fromEntries(Object.entries(VENDOR_URLS).map(([url, file]) => [url, `/${token}/__vendor/${file}`]));
  return `<script data-parity-resources>(function(){var m=${JSON.stringify(map)},seen=false;Object.defineProperty(window,"__resources",{configurable:true,` +
    'get:function(){if(!seen&&window.__dcBoot){seen=true;return undefined}return m}})})()</script>';
}

const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.jsx': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.svg': 'image/svg+xml', '.png': 'image/png',
  '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.gif': 'image/gif', '.webp': 'image/webp', '.woff': 'font/woff',
  '.woff2': 'font/woff2', '.ttf': 'font/ttf', '.otf': 'font/otf', '.json': 'application/json', '.mp4': 'video/mp4', '.webm': 'video/webm',
};

const unreadable = new Set();

// Where a request for `rest` inside a folder really leads: { file } or { fail: status }. A path that leaves the folder is
// refused before the disk is touched, and again once symlinks are resolved. Exported for the script's own checks.
export function insideFolder(folder, rest) {
  const wanted = path.resolve(folder.dir, rest);
  if (!wanted.startsWith(folder.dir + path.sep)) return { fail: 403 };
  let real;
  try {
    real = fs.realpathSync(wanted);
  } catch (error) {
    if (error.code !== 'EACCES') return { fail: 404 };
    unreadable.add(wanted);
    return { fail: 403 };
  }
  if (!real.startsWith(folder.real + path.sep)) return { fail: 403 };
  return { file: real };
}

// A local server: /<token>/r/<id>/… draws with the reference engine, /<token>/o/<id>/… with ours, and
// /<token>/__vendor/<file> are the runtime's own files. An id stands for one board folder of this run; there is no other way
// to name a folder. Anything else is 403.
export function startServer(args, work, boards) {
  const token = randomBytes(16).toString('hex');
  const vendorDir = path.join(REPO, 'internal/design/vendor');
  const vendorFiles = new Map(Object.values(VENDOR_URLS).map((file) => [file, path.join(vendorDir, file)]));
  const boardCopies = path.join(work, 'boards', token);
  const folders = new Map();
  const idOfDir = new Map();
  for (const { dir } of boards) {
    if (idOfDir.has(dir)) continue;
    const id = randomBytes(8).toString('hex');
    idOfDir.set(dir, id);
    folders.set(id, { dir: path.resolve(dir), real: fs.realpathSync(dir) });
  }
  const server = http.createServer((req, res) => {
    const fail = (code) => { res.statusCode = code; res.end(); };
    const sendFile = (file) => {
      // A file this user may not read answers 403 to both engines; it is counted, never worked around.
      try {
        fs.accessSync(file, fs.constants.R_OK);
      } catch {
        unreadable.add(file);
        return fail(403);
      }
      res.setHeader('Content-Type', TYPES[path.extname(file).toLowerCase()] ?? 'application/octet-stream');
      fs.createReadStream(file).on('error', () => res.destroy()).pipe(res);
    };
    try {
      const raw = new URL(req.url, 'http://local').pathname;
      if (!raw.startsWith(`/${token}/`)) return fail(403);
      const pathname = decodeURIComponent(raw.slice(token.length + 1));
      if (pathname.includes('\0')) return fail(403);
      if (pathname.startsWith('/__vendor/')) {
        const file = vendorFiles.get(pathname.slice('/__vendor/'.length));
        return file ? sendFile(file) : fail(403);
      }
      const match = /^\/([ro])\/([^/]+)\/(.*)$/.exec(pathname);
      if (!match) return fail(403);
      const [, engine, id, rest] = match;
      const folder = folders.get(id);
      if (!folder) return fail(403);
      // The engines are the one thing served from outside the folder, and only these two paths.
      if (rest === 'support.js') {
        const chosen = engine === 'o' ? args.engine : args.vendor;
        if (chosen) return sendFile(chosen);
      }
      const place = insideFolder(folder, rest);
      if (place.fail) return fail(place.fail);
      const file = place.file;
      if (fs.statSync(file).isDirectory()) return fail(404);
      if (rest.endsWith('.dc.html') && !rest.includes('/')) {
        try {
          fs.accessSync(file, fs.constants.R_OK);
        } catch {
          unreadable.add(file);
          return fail(403);
        }
        // Boards come from the copies, with the runtime's script in <head>; the file a board fetches is the same text.
        const copy = path.join(boardCopies, id, path.basename(rest));
        if (!fs.existsSync(copy)) {
          fs.mkdirSync(path.dirname(copy), { recursive: true });
          fs.copyFileSync(file, copy);
        }
        res.setHeader('Content-Type', TYPES['.html']);
        res.end(String(fs.readFileSync(copy)).replace('<head>', `<head>${resourcesScript(token)}`));
        return;
      }
      sendFile(file);
    } catch {
      fail(500);
    }
  });
  const cleanup = () => fs.rmSync(boardCopies, { recursive: true, force: true });
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => {
    const origin = `http://127.0.0.1:${server.address().port}`;
    resolve({ server, origin, cleanup, token, urlOf: (board, engine) => `${origin}/${token}/${engine}/${idOfDir.get(board.dir)}/${encodeURIComponent(board.file)}` });
  }));
}

// Addresses a board has no business reaching: this machine, private networks, link-local (cloud metadata included), and the
// forms that embed an IPv4 address in IPv6. Two lists: one BlockList matches IPv4 addresses against its ::ffff:0:0/96 rule too.
const NOT_PUBLIC_V4 = new net.BlockList();
const NOT_PUBLIC_V6 = new net.BlockList();
for (const [address, bits] of [['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10], ['127.0.0.0', 8], ['169.254.0.0', 16], ['172.16.0.0', 12],
  ['192.0.0.0', 24], ['192.0.2.0', 24], ['192.168.0.0', 16], ['198.18.0.0', 15], ['198.51.100.0', 24], ['203.0.113.0', 24], ['224.0.0.0', 4], ['240.0.0.0', 4]]) {
  NOT_PUBLIC_V4.addSubnet(address, bits, 'ipv4');
}
for (const [address, bits] of [['::', 96], ['::ffff:0:0', 96], ['64:ff9b::', 96], ['100::', 64], ['2001:db8::', 32], ['2002::', 16], ['fc00::', 7], ['fe80::', 10], ['fec0::', 10], ['ff00::', 8]]) {
  NOT_PUBLIC_V6.addSubnet(address, bits, 'ipv6');
}

export function addressIsPublic(address) {
  const family = net.isIP(address);
  if (!family) return false;
  return family === 6 ? !NOT_PUBLIC_V6.check(address, 'ipv6') : !NOT_PUBLIC_V4.check(address, 'ipv4');
}

export class BlockedAddress extends Error {}

const MAX_REDIRECTS = 5;
const DECODERS = { gzip: zlib.gunzipSync, 'x-gzip': zlib.gunzipSync, br: zlib.brotliDecompressSync, deflate: zlib.inflateSync };

// One GET with no redirect followed. The check sits in the connection's own name lookup, so that a name cannot answer one
// address to a check and another to the connection.
function requestOnce(url, headers, allowed) {
  return new Promise((resolve, reject) => {
    const host = url.hostname.replace(/^\[|\]$/g, '');
    if (net.isIP(host) && !allowed(host)) return reject(new BlockedAddress(host));
    const lookup = (name, options, done) => {
      dns.lookup(name, { all: true }, (error, list) => {
        if (error) return done(error);
        const refused = list.find((item) => !allowed(item.address));
        if (refused) return done(new BlockedAddress(`${name}: ${refused.address}`));
        if (options.all) return done(null, list);
        return done(null, list[0].address, list[0].family);
      });
    };
    const client = url.protocol === 'https:' ? https : http;
    const request = client.request(url, { method: 'GET', headers, lookup, signal: AbortSignal.timeout(20000) }, (response) => {
      const chunks = [];
      response.on('data', (chunk) => chunks.push(chunk));
      response.on('error', reject);
      response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, body: Buffer.concat(chunks) }));
    });
    request.on('error', (error) => {
      // A connection tries every address it was given and reports them together.
      const inner = error instanceof AggregateError ? error.errors : [error];
      reject(inner.find((item) => item instanceof BlockedAddress) ?? error);
    });
    request.end();
  });
}

// GET of a public http(s) URL, every redirect checked as a new request. Throws BlockedAddress for anything else.
export async function fetchPublic(address, headers, allowed = addressIsPublic) {
  let url = new URL(address);
  for (let hop = 0; hop <= MAX_REDIRECTS; hop += 1) {
    if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new BlockedAddress(url.protocol);
    const answer = await requestOnce(url, headers, allowed);
    const next = answer.headers.location;
    if (answer.status >= 300 && answer.status < 400 && next) {
      url = new URL(next, url);
      continue;
    }
    const encoding = String(answer.headers['content-encoding'] ?? '').trim().toLowerCase();
    if (encoding && encoding !== 'identity') {
      if (!DECODERS[encoding]) throw new Error(`content-encoding ${encoding}`);
      answer.body = DECODERS[encoding](answer.body);
    }
    return answer;
  }
  throw new Error('too many redirects');
}

const refusedAddresses = new Set();

// External requests are answered from a cache of first answers, so both engines see the same bytes. Addresses that are
// not public are refused and never cached. CORS headers are the source's: an origin it echoed back is kept as a mark, because
// the local server's port changes from run to run. The Origin sent is part of the key, as sources answer differently to it.
export function netCache(work, allowed = addressIsPublic) {
  const dir = path.join(work, 'net-cache-v2');
  fs.mkdirSync(dir, { recursive: true });
  const inFlight = new Map();
  const ECHO = '<the origin that asked>';
  return async function answer(route) {
    const url = route.request().url();
    const requestHeaders = route.request().headers();
    const origin = requestHeaders.origin;
    const key = createHash('sha1').update(`${origin ? 'cors' : 'plain'} ${url}`).digest('hex');
    const meta = path.join(dir, `${key}.json`);
    const body = path.join(dir, `${key}.bin`);
    if (!fs.existsSync(meta)) {
      if (!inFlight.has(key)) {
        inFlight.set(key, (async () => {
          try {
            const headers = { accept: '*/*', 'accept-encoding': 'gzip, deflate, br', 'user-agent': requestHeaders['user-agent'] ?? 'Mozilla/5.0 Chrome/140' };
            if (origin) headers.origin = origin;
            const response = await fetchPublic(url, headers, allowed);
            const allowOrigin = response.headers['access-control-allow-origin'];
            fs.writeFileSync(body, response.body);
            fs.writeFileSync(meta, JSON.stringify({
              status: response.status,
              type: response.headers['content-type'],
              allowOrigin: origin && allowOrigin === origin ? ECHO : allowOrigin,
              allowCredentials: response.headers['access-control-allow-credentials'],
            }));
          } catch (error) {
            if (error instanceof BlockedAddress) return { refused: true };
            fs.writeFileSync(meta, JSON.stringify({ failed: String(error) }));
          } finally {
            inFlight.delete(key);
          }
          return {};
        })());
      }
      const outcome = await inFlight.get(key);
      if (outcome.refused) {
        refusedAddresses.add(url);
        return route.abort('blockedbyclient');
      }
    }
    const stored = JSON.parse(fs.readFileSync(meta, 'utf8'));
    if (stored.failed) return route.abort('failed');
    const headers = { 'content-type': stored.type ?? 'application/octet-stream' };
    // Playwright's fulfill adds an allowing CORS header to a cross-origin answer that has none. A source that sent none gets
    // 'null', which allows no real origin: the page cannot read it, and a plain <img> or <script> still loads it.
    headers['access-control-allow-origin'] = (stored.allowOrigin === ECHO ? origin : stored.allowOrigin) ?? 'null';
    if (stored.allowCredentials) headers['access-control-allow-credentials'] = stored.allowCredentials;
    return route.fulfill({ status: stored.status, headers, body: fs.readFileSync(body) });
  };
}

// Runs in the page. The normalized DOM: one line per node, attributes sorted, adjacent text merged, comments dropped.
// Taken out of the comparison, and only these: the harness's own script, the engine's base stylesheet (each engine has
// its own text, it is compared by its effect on the picture), the script tags that load React and Babel (a reference
// engine may bundle them) and the order of the rules of the atomics sheet.
function normalizeDomInPage() {
  const libScript = /(\/__vendor\/|unpkg\.com\/(react|react-dom)@|unpkg\.com\/@babel\/standalone@)/;
  const lines = [];
  const clean = (text) => text.replace(/(\/[0-9a-f]{32})?\/[ro]\/[0-9a-f]{16}\//g, '/P/');
  const walk = (node, depth) => {
    let text = '';
    const flush = () => {
      if (text !== '') lines.push(`${' '.repeat(depth)}#${JSON.stringify(clean(text))}`);
      text = '';
    };
    for (let child = node.firstChild; child; child = child.nextSibling) {
      if (child.nodeType === 3) { text += child.nodeValue; continue; }
      if (child.nodeType !== 1) continue;
      const tag = child.localName;
      if (tag === 'script' && (child.hasAttribute('data-parity-resources') || libScript.test(child.getAttribute('src') ?? ''))) continue;
      if (tag === 'style' && child.parentNode === document.head && (child.textContent.includes('.sc-placeholder{') || child.textContent.includes('.sc-placeholder {'))) continue;
      flush();
      const attrs = child.getAttributeNames().sort().map((name) => `${name}=${JSON.stringify(clean(child.getAttribute(name)))}`);
      lines.push(`${' '.repeat(depth)}<${tag}${attrs.length ? ` ${attrs.join(' ')}` : ''}>`);
      if (tag === 'style' && child.id === '__dc-atomics') {
        lines.push(`${' '.repeat(depth + 1)}#${child.textContent.split('}').sort().join('}')}`);
        continue;
      }
      if (tag === 'template' && child.content) walk(child.content, depth + 1);
      walk(child, depth + 1);
    }
    flush();
  };
  const html = document.documentElement;
  lines.push(`<html ${html.getAttributeNames().sort().map((n) => `${n}=${JSON.stringify(html.getAttribute(n))}`).join(' ')}>`);
  lines.push(`title=${JSON.stringify(document.title)}`);
  walk(html, 1);
  const tpl = {};
  for (const el of document.querySelectorAll('#dc-root [data-dc-tpl]')) {
    const n = el.getAttribute('data-dc-tpl');
    tpl[n] = (tpl[n] ?? 0) + 1;
  }
  return { lines, tpl };
}

// Determinism: the same Math.random and the same Date on both engines.
const DETERMINISM = `(() => {
  let seed = 0x2F6E2B1;
  Math.random = () => { seed |= 0; seed = seed + 0x6D2B79F5 | 0; let t = Math.imul(seed ^ seed >>> 15, 1 | seed); t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t; return ((t ^ t >>> 14) >>> 0) / 4294967296; };
  window.open = () => null;
})();`;

function normalizeMessage(text) {
  return text
    .replace(/https?:\/\/127\.0\.0\.1:\d+/g, '')
    .replace(/(\/[0-9a-f]{32})?\/[ro]\/[0-9a-f]{16}\//g, '/P/')
    .replace(/\/[0-9a-f]{32}\/__vendor\//g, '/__vendor/')
    .replace(/0x[0-9a-f]+/gi, '0x')
    .split('\n')[0]
    .replace(/\s+at\s.*$/, '')
    .trim();
}

// A page that hangs (a never-ending promise, a blocked main thread) must not stop the run: a draw has a hard limit.
const DRAW_LIMIT_MS = 150000;

function withLimit(promise, ms, what) {
  let timer;
  const limit = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`${what}: over ${ms} ms`)), ms); });
  return Promise.race([promise, limit]).finally(() => clearTimeout(timer));
}

async function drawBoard(browser, args, site, cache, board, engine, wantSteps) {
  const { origin } = site;
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, reducedMotion: 'no-preference' });
  const out = { errors: [], warnings: [], failed: false };
  const draw = async () => {
    await context.route((url) => url.origin !== origin, cache);
    await context.addInitScript(DETERMINISM);
    const page = await context.newPage();
    await page.clock.setFixedTime(new Date('2026-06-15T12:00:00Z'));
    page.on('console', (m) => {
      const type = m.type();
      if (type === 'error') out.errors.push(normalizeMessage(m.text()));
      else if (type === 'warning') out.warnings.push(normalizeMessage(m.text()));
    });
    page.on('pageerror', (e) => out.errors.push(`pageerror: ${normalizeMessage(String(e))}`));
    await page.goto(site.urlOf(board, engine), { waitUntil: 'load', timeout: 60000 });
    await page.waitForSelector('#dc-root', { state: 'attached', timeout: 30000 }).catch(() => { out.noRoot = true; });
    // Settled: two equal looks at the DOM half a second apart, the fonts done; a board that never settles is marked.
    const started = Date.now();
    let last = null;
    out.settled = false;
    while (Date.now() - started < args.settleMs) {
      const now = await page.evaluate(() => (document.getElementById('dc-root')?.outerHTML.length ?? 0) + ':' + document.querySelectorAll('*').length);
      // At least three seconds: a board's own timers (a skeleton fading out, a counter) are part of how it draws.
      if (now === last && Date.now() - started > 3000) { out.settled = true; break; }
      last = now;
      await page.waitForTimeout(500);
    }
    // What the board still loads (stylesheets, a nested iframe) is part of the picture: wait for it on both engines alike.
    await page.waitForLoadState('networkidle', { timeout: args.settleMs }).catch(() => { out.networkBusy = true; });
    // Network quiet is not the end of a nested page: a long script runs with no request open. Every frame must finish
    // loading and its fonts, on both engines alike; a frame that does not finish within the limit is marked.
    const framesStarted = Date.now();
    out.framesBusy = true;
    while (Date.now() - framesStarted < args.settleMs) {
      const done = await Promise.all(page.frames().map((frame) => frame.evaluate(() => document.readyState === 'complete').catch(() => true)));
      if (done.every(Boolean)) { out.framesBusy = false; break; }
      await page.waitForTimeout(250);
    }
    await Promise.all(page.frames().map((frame) => withLimit(frame.evaluate(() => document.fonts.ready.then(() => true)), 10000, 'fonts').catch(() => {})));
    await page.waitForLoadState('networkidle', { timeout: args.settleMs }).catch(() => { out.networkBusy = true; });
    const dom = await page.evaluate(normalizeDomInPage);
    out.dom = dom.lines;
    out.tpl = dom.tpl;
    // A board whose endless animations cannot be stopped within the limit is taken as it is, and marked.
    out.png = await page.screenshot({ fullPage: true, animations: 'disabled', caret: 'hide', timeout: 30000 }).catch(() => {
      out.pictureLive = true;
      return page.screenshot({ fullPage: true, animations: 'allow', caret: 'hide', timeout: 30000 });
    });
    if (wantSteps) out.steps = await interact(page);
  };
  try {
    await withLimit(draw(), DRAW_LIMIT_MS, `draw on ${engine}`);
  } catch (error) {
    out.failed = true;
    out.failReason = normalizeMessage(String(error));
  } finally {
    await withLimit(context.close(), 15000, 'close').catch(() => {});
  }
  return out;
}

// The interactive part: each tweak to its other values, then the first controls clicked; the DOM after each step.
async function interact(page) {
  const steps = [];
  const look = async (label) => {
    await page.waitForTimeout(150);
    const dom = await page.evaluate(normalizeDomInPage);
    steps.push({ label, dom: dom.lines });
  };
  const tweaks = await page.evaluate(() => {
    const name = window.__dcRootName?.();
    const meta = window.__dcRegistry?.[name]?.propsMeta ?? {};
    const out = [];
    for (const [key, item] of Object.entries(meta)) {
      const values = [];
      if (Array.isArray(item?.options)) values.push(...item.options.filter((o) => o !== item.default).slice(0, 2));
      else if (typeof item?.default === 'boolean') values.push(!item.default);
      for (const value of values) out.push({ name, key, value });
    }
    return out.slice(0, 8);
  });
  for (const tweak of tweaks) {
    await page.evaluate(({ name, key, value }) => window.__dcSetProps(name, { [key]: value }), tweak);
    await look(`tweak ${tweak.key}=${JSON.stringify(tweak.value)}`);
  }
  if (tweaks.length) {
    await page.evaluate((name) => window.__dcSetProps(name, {}), tweaks[0].name);
    await look('tweaks reset');
  }
  const controls = 'button,[role=button],[role=tab],[role=switch],[role=checkbox],input[type=checkbox],summary';
  const count = await page.evaluate((selector) => document.querySelectorAll(`#dc-root ${selector}`).length, controls);
  for (let i = 0; i < Math.min(count, 5); i += 1) {
    const clicked = await page.evaluate(({ selector, i }) => {
      const el = document.querySelectorAll(`#dc-root ${selector}`)[i];
      if (!el) return null;
      el.click();
      return el.localName + ':' + (el.textContent ?? '').trim().slice(0, 30);
    }, { selector: controls, i });
    await look(`click ${i} ${clicked}`);
  }
  return steps;
}

function decodePng(buffer) {
  let at = 8;
  let width = 0, height = 0, channels = 0, depth = 0;
  const data = [];
  while (at < buffer.length) {
    const length = buffer.readUInt32BE(at);
    const type = buffer.toString('latin1', at + 4, at + 8);
    if (type === 'IHDR') {
      width = buffer.readUInt32BE(at + 8);
      height = buffer.readUInt32BE(at + 12);
      depth = buffer[at + 16];
      channels = { 0: 1, 2: 3, 4: 2, 6: 4 }[buffer[at + 17]];
    } else if (type === 'IDAT') data.push(buffer.subarray(at + 8, at + 8 + length));
    at += 12 + length;
  }
  if (depth !== 8 || !channels) throw new Error('unsupported PNG');
  const raw = zlib.inflateSync(Buffer.concat(data));
  const stride = width * channels;
  const out = Buffer.alloc(stride * height);
  for (let y = 0; y < height; y += 1) {
    const filter = raw[y * (stride + 1)];
    const line = y * (stride + 1) + 1;
    for (let x = 0; x < stride; x += 1) {
      const a = x >= channels ? out[y * stride + x - channels] : 0;
      const b = y > 0 ? out[(y - 1) * stride + x] : 0;
      const c = x >= channels && y > 0 ? out[(y - 1) * stride + x - channels] : 0;
      let predicted = 0;
      if (filter === 1) predicted = a;
      else if (filter === 2) predicted = b;
      else if (filter === 3) predicted = (a + b) >> 1;
      else if (filter === 4) {
        const p = a + b - c;
        const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
        predicted = pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
      }
      out[y * stride + x] = (raw[line + x] + predicted) & 255;
    }
  }
  return { width, height, channels, pixels: out };
}

// Pixels that differ in any channel; a difference of size is reported as such.
function comparePngs(a, b) {
  const left = decodePng(a);
  const right = decodePng(b);
  if (left.width !== right.width || left.height !== right.height) {
    return { same: false, size: `${left.width}x${left.height} vs ${right.width}x${right.height}`, differing: null, ratio: 1 };
  }
  let differing = 0;
  const n = left.width * left.height;
  for (let i = 0; i < n; i += 1) {
    for (let c = 0; c < left.channels; c += 1) {
      if (left.pixels[i * left.channels + c] !== right.pixels[i * right.channels + c]) { differing += 1; break; }
    }
  }
  return { same: differing === 0, differing, ratio: differing / n };
}

function firstDifference(a, b) {
  const n = Math.max(a.length, b.length);
  for (let i = 0; i < n; i += 1) {
    if (a[i] !== b[i]) return { line: i, reference: (a[i] ?? '<end>').slice(0, 300), ours: (b[i] ?? '<end>').slice(0, 300) };
  }
  return null;
}

function countMismatch(a, b) {
  let n = Math.abs(a.length - b.length);
  for (let i = 0; i < Math.min(a.length, b.length); i += 1) if (a[i] !== b[i]) n += 1;
  return n;
}

function setDifference(a, b) {
  const known = new Set(a);
  return [...new Set(b)].filter((x) => !known.has(x));
}

function compareSteps(reference, ours) {
  const out = { total: 0, same: 0, firstDiff: null };
  if (!reference || !ours) return out;
  const n = Math.max(reference.length, ours.length);
  for (let i = 0; i < n; i += 1) {
    out.total += 1;
    const r = reference[i];
    const o = ours[i];
    if (r && o && r.label === o.label && JSON.stringify(r.dom) === JSON.stringify(o.dom)) out.same += 1;
    else if (!out.firstDiff) out.firstDiff = { step: r?.label ?? o?.label, ourLabel: o?.label, ...(r && o ? firstDifference(r.dom, o.dom) : {}) };
  }
  return out;
}

async function runBoard(browser, args, site, cache, board) {
  const record = { board: board.id };
  const reference = await drawBoard(browser, args, site, cache, board, 'r', args.interact);
  const ours = await drawBoard(browser, args, site, cache, board, 'o', args.interact);
  record.referenceFailed = reference.failed ? reference.failReason : undefined;
  record.oursFailed = ours.failed ? ours.failReason : undefined;
  record.settled = { reference: reference.settled, ours: ours.settled };
  if (reference.failed || ours.failed) return record;

  record.domSame = JSON.stringify(reference.dom) === JSON.stringify(ours.dom);
  if (!record.domSame) {
    record.domDiff = firstDifference(reference.dom, ours.dom);
    record.domDiffLines = countMismatch(reference.dom, ours.dom);
    record.domLines = reference.dom.length;
    const safe = createHash('sha1').update(board.id).digest('hex').slice(0, 12);
    const dir = path.join(args.work, 'diff');
    fs.mkdirSync(dir, { recursive: true });
    record.domFiles = { reference: path.join(dir, `${safe}.ref.txt`), ours: path.join(dir, `${safe}.own.txt`) };
    fs.writeFileSync(record.domFiles.reference, reference.dom.join('\n'));
    fs.writeFileSync(record.domFiles.ours, ours.dom.join('\n'));
  }
  record.tplSame = JSON.stringify(reference.tpl) === JSON.stringify(ours.tpl);
  record.newErrors = setDifference(reference.errors, ours.errors);
  record.goneErrors = setDifference(ours.errors, reference.errors);
  record.newWarnings = setDifference(reference.warnings, ours.warnings);
  record.goneWarnings = setDifference(ours.warnings, reference.warnings);
  record.picture = comparePngs(reference.png, ours.png);
  if (!record.picture.same) {
    const safe = createHash('sha1').update(board.id).digest('hex').slice(0, 12);
    const dir = path.join(args.work, 'diff');
    fs.mkdirSync(dir, { recursive: true });
    record.shots = { reference: path.join(dir, `${safe}.ref.png`), ours: path.join(dir, `${safe}.own.png`) };
    fs.writeFileSync(record.shots.reference, reference.png);
    fs.writeFileSync(record.shots.ours, ours.png);
  }
  if (args.interact) record.steps = compareSteps(reference.steps, ours.steps);

  const needControl = args.control && (!record.domSame || !record.picture.same || (record.steps && record.steps.same !== record.steps.total));
  if (needControl) {
    const again = await drawBoard(browser, args, site, cache, board, 'r', args.interact);
    if (again.failed) record.control = { failed: again.failReason };
    else {
      record.control = {
        domSame: JSON.stringify(reference.dom) === JSON.stringify(again.dom),
        domDiffLines: countMismatch(reference.dom, again.dom),
        pictureSame: comparePngs(reference.png, again.png).same,
      };
      if (args.interact) record.control.stepsSame = compareSteps(reference.steps, again.steps).same === (reference.steps?.length ?? 0);
    }
  }
  return record;
}

function summarize(records, wantSteps) {
  const s = {
    boards: records.length,
    referenceFailed: records.filter((r) => r.referenceFailed).length,
    oursFailed: records.filter((r) => r.oursFailed).length,
    domSame: records.filter((r) => r.domSame).length,
    tplSame: records.filter((r) => r.tplSame).length,
    pictureSame: records.filter((r) => r.picture?.same).length,
    pictureUnderOnePercent: records.filter((r) => r.picture && !r.picture.same && r.picture.ratio < 0.01).length,
    newErrors: records.filter((r) => r.newErrors?.length).length,
    newWarnings: records.filter((r) => r.newWarnings?.length).length,
    domDiffersAndReferenceStable: records.filter((r) => r.domSame === false && r.control?.domSame === true).length,
    domDiffersAndReferenceNoisy: records.filter((r) => r.domSame === false && r.control?.domSame === false).length,
    pictureDiffersAndReferenceStable: records.filter((r) => r.picture && !r.picture.same && r.control?.pictureSame === true).length,
    pictureDiffersAndReferenceNoisy: records.filter((r) => r.picture && !r.picture.same && r.control?.pictureSame === false).length,
    notSettled: records.filter((r) => r.settled && (!r.settled.reference || !r.settled.ours)).length,
  };
  if (wantSteps) {
    s.stepsTotal = records.reduce((n, r) => n + (r.steps?.total ?? 0), 0);
    s.stepsSame = records.reduce((n, r) => n + (r.steps?.same ?? 0), 0);
    s.boardsWithStepDiffs = records.filter((r) => r.steps && r.steps.same !== r.steps.total).length;
    s.boardsWithStepDiffsReferenceStable = records.filter((r) => r.steps && r.steps.same !== r.steps.total && r.control?.stepsSame === true).length;
  }
  return s;
}

function lineFor(r) {
  if (r.oursFailed) return `FAIL ours       ${r.board}: ${r.oursFailed}`;
  if (r.referenceFailed) return `FAIL reference  ${r.board}: ${r.referenceFailed}`;
  const marks = [
    r.domSame ? 'dom=' : `dom!${r.domDiffLines}/${r.domLines}`,
    r.tplSame ? 'tpl=' : 'tpl!',
    r.picture.same ? 'px=' : `px!${r.picture.size ?? `${(r.picture.ratio * 100).toFixed(3)}%`}`,
    r.newErrors.length ? `err+${r.newErrors.length}` : 'err=',
  ];
  if (r.steps) marks.push(`steps ${r.steps.same}/${r.steps.total}`);
  if (r.control) marks.push(`ctl dom${r.control.domSame ? '=' : `!${r.control.domDiffLines}`} px${r.control.pictureSame ? '=' : '!'}${r.control.stepsSame === undefined ? '' : ` steps${r.control.stepsSame ? '=' : '!'}`}`);
  return `${marks.join(' ').padEnd(44)} ${r.board}`;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  args.corpus = path.resolve(args.corpus);
  args.work = path.resolve(args.work);
  fs.mkdirSync(args.work, { recursive: true });
  if (!fs.existsSync(args.engine)) throw new Error(`no engine at ${args.engine}`);
  if (args.vendor) args.vendor = path.resolve(args.vendor);

  let boards = findBoards(args.corpus);
  if (args.filter) {
    const re = new RegExp(args.filter);
    boards = boards.filter((b) => re.test(b.id));
  }
  if (args.sample) boards = sample(boards, Number(args.sample));
  if (!boards.length) throw new Error('no boards');

  const { chromium } = loadPlaywright(args);
  const launch = () => chromium.launch({ headless: true, executablePath: args.chromium });
  const site = await startServer(args, args.work, boards);
  const cache = netCache(args.work);
  const records = [];
  const out = args.out ?? path.join(args.work, 'parity.json');
  let next = 0;

  const worker = async () => {
    let browser = await launch();
    let drawn = 0;
    try {
      for (;;) {
        const i = next;
        next += 1;
        if (i >= boards.length) return;
        if (drawn >= args.restartEvery) {
          await browser.close().catch(() => {});
          browser = await launch();
          drawn = 0;
        }
        drawn += 1;
        const record = await runBoard(browser, args, site, cache, boards[i]).catch((error) => ({ board: boards[i].id, oursFailed: `harness: ${String(error)}` }));
        records.push(record);
        console.log(`[${String(records.length).padStart(3)}/${boards.length}] ${lineFor(record)}`);
        fs.writeFileSync(out, JSON.stringify({ summary: summarize(records, args.interact), records }, null, 1));
      }
    } finally {
      await browser.close().catch(() => {});
    }
  };
  await Promise.all(Array.from({ length: Math.max(1, args.pages) }, worker));
  site.server.close();
  site.cleanup();

  const summary = summarize(records, args.interact);
  fs.writeFileSync(out, JSON.stringify({ summary, records }, null, 1));
  console.log('\n--- summary ---');
  for (const [key, value] of Object.entries(summary)) console.log(`${key.padEnd(36)} ${value}`);
  if (unreadable.size) console.log(`\nfiles of the corpus this user cannot read (403 to both engines): ${unreadable.size}`);
  if (refusedAddresses.size) console.log(`requests to addresses that are not public, refused: ${refusedAddresses.size}`);
  console.log(`\ndetails: ${out}`);
}

if (process.argv[1] && fs.realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(String(error));
    process.exit(2);
  });
}
