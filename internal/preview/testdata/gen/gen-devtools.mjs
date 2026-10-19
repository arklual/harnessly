// The preview DevTools of the previous runtime driven through its own API dispatcher and handlers: a script of store
// changes (requests the proxy logged, the page's reports, settings) interleaved with the agents' and the pane's HTTP
// calls and MCP messages. Writes ../devtools.json: every step and what each call answered. The Go test replays the
// script on its routes and must answer the same bytes.
//
//   OLD_RUNTIME_SRC=<checkout> NODE_MODULES=<its node_modules> PRODUCT=… PRODUCT_DOMAIN=… CLI=… \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen-devtools.mjs
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { createHash, createHmac } from 'node:crypto';
import { FIXED, rng, neutral } from './common.mjs';

let clock = FIXED;
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(clock); }
  static now() { return clock; }
};
const SCALE = Number(process.env.GOLDEN_SCALE ?? 1);
const R = rng(Number(process.env.GOLDEN_SEED ?? 3008));
const P = process.env.PRODUCT;
const data = fs.mkdtempSync(path.join(os.tmpdir(), 'preview-devtools-'));
process.env.GOLDEN_DATA_DIR = data;
const SECRET = 'golden-pair-secret';
process.env.NEXTAUTH_SECRET = SECRET;
const TOKEN = 'b'.repeat(64);
fs.writeFileSync(path.join(data, 'cli-token'), TOKEN);
globalThis.__golden = { panes: { 'hs-ws-a-pane-b-tab-c': { pid: 4242 } }, capture: {}, captured: [], listeners: {} };
globalThis.__golden.capture['hs-ws-a-pane-b-tab-c'] = ['> dev', 'ERROR in ./src/app.ts', '    at x (a.ts:1:2)', 'compiled successfully', 'warning: slow', 'info'].join('\n');
globalThis.__golden.listeners[5173] = { port: 5173, host: '127.0.0.1', pid: 4242, command: 'node' };
globalThis.__golden.listeners[4300] = { port: 4300, host: '0.0.0.0', pid: null, command: null };

const lib = (rel) => import(`@/lib/${P}-${rel}`);
const store = await lib('preview/devtools/store');
const endpoints = await lib('preview/devtools/endpoints');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const routeNames = ['/api/cli/preview/slots', '/api/cli/preview/network', '/api/cli/preview/console', '/api/cli/preview/server-log', '/api/cli/preview/settings',
  '/api/cli/preview/clear', '/api/cli/preview/mcp', `/api/${P}/preview/devtools`, `/api/${P}/preview/devtools/server-log`];
const dispatch = createApiDispatcher(routeNames.map((route) => ({ route, load: () => import(`@/pages${route}`) })));
const server = http.createServer((req, res) => {
  if (req.url.startsWith('/__slot/')) {
    const slot = req.headers['x-golden-slot'];
    const target = JSON.parse(req.headers['x-golden-target']);
    void endpoints.answerDevtools(req, res, req.url.slice('/__slot'.length), { slot, target });
    return;
  }
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const b64 = (v) => Buffer.from(v).toString('base64url');
const session = (() => {
  const head = `${b64(JSON.stringify({ alg: 'HS256' }))}.${b64(JSON.stringify({ sub: 'user', iat: Math.floor(FIXED / 1000), exp: Math.floor(FIXED / 1000) + 7 * 86400 }))}`;
  return `${head}.${createHmac('sha256', SECRET).update(head).digest('base64url')}`;
})();
const COOKIE = `session-token=${session}`;
const TOKEN_HEADER = `x-${P}-token`;

const KEEP = ['content-type', 'cache-control', 'allow', 'etag', 'x-content-type-options', 'x-robots-tag'];
const steps = [];
const call = async (method, url, headers = {}, body) => {
  if (method === 'GET' || method === 'HEAD') body = undefined;
  const res = await fetch(base + url, { method, headers, body });
  const text = await res.text();
  const kept = {};
  for (const k of KEEP) if (res.headers.get(k) !== null) kept[k] = res.headers.get(k);
  // An answer that spells a product value gets its tag recomputed over the neutral bytes it is written as.
  if (kept.etag && neutral(text) !== text) kept.etag = `"${createHash('sha1').update(neutral(text)).digest('base64url').slice(0, 27)}"`;
  // A long run of one character is written as the character and its count.
  const kept_ = typeof body === 'string' && body.length > 4096 && /^(.)\1*$/s.test(body) ? { repeat: body[0], n: body.length } : body ?? null;
  steps.push({ kind: 'http', method, url, headers, body: kept_, out: { status: res.status, headers: kept, body: text }, captured: globalThis.__golden.captured.splice(0) });
};

const SLOTS = ['https://ai-alice-1.example.org', 'https://ai-alice-2.example.org', 'https://ai-bob-3.example.org'];
const TARGETS = [{ kind: 'port', port: 5173 }, { kind: 'url', origin: 'https://site.example.org' }, { kind: 'port', port: 4300 }];
const op = (o) => { steps.push({ kind: 'op', ...o }); };
const tick = (ms) => { clock += ms; op({ op: 'clock', at: clock }); };

const touch = (i) => { store.touchSlot(SLOTS[i], TARGETS[i]); op({ op: 'touch', slot: SLOTS[i], target: TARGETS[i] }); };
const TYPES = ['document', 'script', 'style', 'image', 'font', 'media', 'fetch', 'xhr', 'beacon', 'websocket', 'manifest', 'other'];
const pending = [];
const request = (i) => {
  const entry = { at: clock, source: 'proxy', method: R.pick(['GET', 'POST', 'HEAD']), url: `http://localhost:5173/${R.pick(['', 'app.js', 'api/orders', 'img.png', `p${R.int(100)}`])}${R.bool(0.03) ? 'x'.repeat(2100) : ''}`,
    host: 'localhost:5173', type: R.pick(TYPES), status: null, mime: null, size: null, durationMs: null, thirdParty: false, blocked: false, throttled: R.bool(0.2), error: null };
  const id = store.recordRequest(SLOTS[i], entry);
  op({ op: 'request', slot: SLOTS[i], entry, id });
  pending.push([i, id]);
};
const finish = () => {
  if (!pending.length) return;
  const [i, id] = pending.splice(R.int(pending.length), 1)[0];
  const patch = R.bool(0.7)
    ? { status: R.pick([200, 304, 404, 500]), mime: R.pick(['text/html', 'application/javascript', null, 'x'.repeat(300)]), size: R.int(5000), durationMs: R.int(900), blocked: false, error: null }
    : { status: null, mime: null, size: null, durationMs: R.int(50), blocked: R.bool(), error: R.pick(['ECONNREFUSED', 'aborted', 'offline', 'blocked: posthog']) };
  if (R.bool(0.3)) patch.type = R.pick(TYPES);
  store.finishRequest(SLOTS[i], id, patch);
  op({ op: 'finish', slot: SLOTS[i], id, patch });
};
const consoleEntry = (i) => {
  const entry = { at: clock, level: R.pick(['error', 'warn', 'info', 'log']), source: R.pick(['console', 'exception', 'rejection', 'network']), text: R.bool(0.05) ? 'x'.repeat(4100) : R.pick(['boom', 'boom', 'hello é']), stack: R.bool(0.05) ? 'y'.repeat(4100) : R.pick([null, 'at a (x:1:2)']), url: R.pick([null, 'http://localhost:5173/']) };
  store.recordConsole(SLOTS[i], entry);
  op({ op: 'console', slot: SLOTS[i], entry });
};

const reportEvent = (slot) => R.bool(0.5)
  ? { kind: 'network', at: R.pick([clock, clock + 10 * 60_000, 'x', clock - 1.5]), method: R.pick(['GET', 'get', 'POST', 'VERYLONGMETHODNAMEX']), url: R.pick([`${slot}/__tk/p/4200/a`, 'https://cdn.example/x.js', 'ftp://x', 'nope', `${slot}/api`]),
    type: R.pick([...TYPES, 'bogus']), status: R.pick([200, 0, 404, 1000, 2.5, '200']), mime: R.pick(['text/html; charset=utf-8', 'bad mime', 'APPLICATION/JSON', null]), size: R.pick([10, -1, 1e13, 3.7]),
    durationMs: R.pick([5, 4e6]), thirdParty: R.pick([true, 'yes', false]), blocked: R.bool(), throttled: R.bool(), error: R.pick([null, 'e'.repeat(250), 'TypeError']) }
  : { kind: R.pick(['console', 'console', 'other']), at: clock, level: R.pick(['error', 'warn', 'info', 'log', 'debug']), source: R.pick(['console', 'exception', 'rejection', 'network', 'x']),
    text: R.bool(0.05) ? 'z'.repeat(8100) : R.pick([`fail at ${slot}/__tk/p/4200/x.js:1:2`, 5, 'same', 'same']), stack: R.pick([undefined, `${slot}/a.js`, 7]), url: R.pick([undefined, `${slot}/page`, 'nope']) };
const report = async (i, opts = {}) => {
  const slot = SLOTS[i];
  const events = [];
  for (let k = 0; k < (opts.many ?? 1 + R.int(6)); k++) events.push(reportEvent(slot));
  const body = opts.raw ?? JSON.stringify(R.bool(0.05) ? { nope: 1 } : { events });
  const headers = { 'x-golden-slot': slot, 'x-golden-target': JSON.stringify(TARGETS[i]), 'content-type': 'application/json' };
  if (opts.origin !== undefined) headers.origin = opts.origin;
  await call(opts.method ?? 'POST', `/__slot${opts.path ?? '/__tk/devtools/report'}`, headers, opts.method === 'GET' ? undefined : body);
};

const qs = (o) => { const s = Object.entries(o).map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&'); return s ? `?${s}` : ''; };
const randQuery = (kind) => {
  const q = {};
  if (R.bool(0.4)) q.slot = R.pick([...SLOTS, 'https://ai-nobody-9.example.org', 'https://evil.example', `${SLOTS[0]}/`, SLOTS[0].toUpperCase(), '']);
  if (R.bool(0.3)) q.since = R.pick(['0', '3', '-1', '1.5', 'abc', '', '1e2', ' 4', '9007199254740993']);
  if (R.bool(0.3)) q.limit = R.pick(['1', '2', '500', '501', '0', 'x']);
  if (kind === 'network') {
    if (R.bool(0.3)) q.errors = R.pick(['1', 'true', '0']);
    if (R.bool(0.2)) q.thirdParty = R.pick(['1', 'yes']);
    if (R.bool(0.3)) q.url = R.pick(['API', 'app.js', '']);
    if (R.bool(0.3)) q.type = R.pick(['script', 'script, image', 'bogus', ',fetch,', 'DOCUMENT']);
  }
  if (kind === 'console' && R.bool(0.4)) q.level = R.pick(['warn', 'ERROR', 'debug', '']);
  if (kind === 'server-log') { if (R.bool(0.4)) q.all = R.pick(['1', 'true', 'no']); if (R.bool(0.4)) q.lines = R.pick(['5', '0', '1001', 'x', '1000']); }
  return q;
};
const tok = { [TOKEN_HEADER]: TOKEN };
const cliRead = async (kind) => call('GET', `/api/cli/preview/${kind}${qs(randQuery(kind))}`, R.bool(0.95) ? tok : {});
const settingsPatch = () => {
  const body = {};
  if (R.bool(0.4)) body.throttle = R.pick([{ preset: 'slow-4g' }, { preset: 'custom', latencyMs: 100.6, downKbps: 1e9, upKbps: 3 }, { preset: 'custom', latencyMs: -1, downKbps: 1, upKbps: 1 }, { preset: 'none' }, 'x', { preset: 'nope' }, { preset: 'offline' }]);
  if (R.bool(0.4)) body.blocked = R.pick([['posthog', ' posthog ', '', 'a'.repeat(301), 5], [], 'x', Array.from({ length: 60 }, (_, k) => `p${k}`)]);
  if (R.bool(0.3)) body.blockedAdd = R.pick([['ga'], ['ga', 'ga'], [' '], 'x', ['*.google-analytics.com/*']]);
  if (R.bool(0.3)) body.blockedRemove = R.pick([['ga'], ['missing'], []]);
  if (R.bool(0.2)) body.disableCache = R.pick([true, false, 'yes']);
  return R.bool(0.05) ? R.pick([[], 'str', null]) : body;
};

const TOOLS = ['preview_slots', 'preview_network', 'preview_console', 'preview_server_log', 'preview_get_settings', 'preview_set_throttle', 'preview_block', 'preview_set_cache', 'preview_clear', 'nope'];
const randArgs = (tool) => {
  const a = {};
  const maybe = (k, vals, p = 0.4) => { if (R.bool(p)) a[k] = R.pick(vals); };
  maybe('slot', [...SLOTS, 'https://ai-x-1.example.org', 3, null], 0.3);
  switch (tool) {
    case 'preview_network': maybe('since', [0, 2, 1.5, -1, 2 ** 60, 'a']); maybe('errors', [true, false, 'x']); maybe('third_party', [true, 1]); maybe('url', ['api', 5]); maybe('type', [['script'], ['x', 'image'], 'script', [3]]); maybe('limit', [1, 0, 500, 501, 2.5]); break;
    case 'preview_console': maybe('since', [0, 3]); maybe('level', ['warn', 'ERROR', 1]); maybe('limit', [2, 600]); break;
    case 'preview_server_log': maybe('all', [true, 'yes']); maybe('lines', [5, 0, 1001, 1.5]); break;
    case 'preview_set_throttle': maybe('preset', ['slow-4g', 'custom', 'none', 'bad'], 0.9); maybe('latency_ms', [10, -1, 70000, 1.5]); maybe('down_kbps', [100, -5]); maybe('up_kbps', [50, 'x']); break;
    case 'preview_block': maybe('add', [['ga'], [''], ['a'.repeat(301)], 'x', [' ']]); maybe('remove', [['ga'], []]); maybe('clear', [true, false, 'y']); break;
    case 'preview_set_cache': maybe('disable_cache', [true, false, null], 0.8); break;
    default: break;
  }
  if (R.bool(0.1)) a[R.pick(['zz', '5', 'extra'])] = 1;
  return a;
};
const mcpMessage = () => {
  const id = R.pick([1, 'abc', 2.5, null, undefined, {}]);
  const kind = R.pick(['call', 'call', 'call', 'list', 'init', 'ping', 'bogus', 'notify', 'response', 'invalid']);
  if (kind === 'invalid') return R.pick([5, 'x', null, { jsonrpc: '1.0', id: 1, method: 'ping' }, { jsonrpc: '2.0', id: 1 }]);
  if (kind === 'response') return { jsonrpc: '2.0', id: 1, result: {} };
  if (kind === 'notify') return { jsonrpc: '2.0', method: 'notifications/initialized' };
  const m = { jsonrpc: '2.0', id, method: { call: 'tools/call', list: 'tools/list', init: 'initialize', ping: 'ping', bogus: 'resources/list' }[kind] };
  if (id === undefined) delete m.id;
  if (kind === 'init') m.params = R.pick([{ protocolVersion: '2025-06-18' }, { protocolVersion: '1999-01-01' }, {}, undefined]);
  if (kind === 'call') {
    const tool = R.pick(TOOLS);
    m.params = R.bool(0.05) ? R.pick([undefined, { name: 5 }, { name: tool, arguments: [] }, { name: tool, arguments: null }]) : { name: tool, ...(R.bool(0.9) ? { arguments: randArgs(tool) } : {}) };
  }
  return m;
};
const mcp = async () => {
  let body;
  const shape = R.int(20);
  if (shape === 0) body = '{bad json';
  else if (shape === 1) body = JSON.stringify(Array.from({ length: R.pick([0, 21, 3]) }, mcpMessage));
  else if (shape === 2) body = JSON.stringify([mcpMessage(), mcpMessage()]);
  else body = JSON.stringify(mcpMessage());
  const headers = { ...tok, 'content-type': R.pick(['application/json', 'application/json', 'application/json; charset=utf-8', 'text/plain']) };
  if (R.bool(0.03)) delete headers[TOKEN_HEADER];
  await call(R.bool(0.03) ? 'GET' : 'POST', '/api/cli/preview/mcp', headers, body);
};

// The script.
await call('GET', '/api/cli/preview/slots', tok);
await cliRead('network');
await mcp();
await call('POST', '/api/cli/preview/mcp', { ...tok, 'content-type': 'application/json' }, JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-03-26' } }));
await call('POST', '/api/cli/preview/mcp', { ...tok, 'content-type': 'application/json' }, JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/list' }));
await call('POST', '/api/cli/preview/mcp', { ...tok, 'content-type': 'application/json' }, 'x'.repeat(300 * 1024));
await call('GET', `/api/${P}/preview/devtools`, { cookie: COOKIE });
for (let i = 0; i < 3; i++) { touch(i); tick(1000); }
const rounds = Math.round(150 * SCALE);
for (let r = 0; r < rounds; r++) {
  const i = R.int(3);
  const action = R.int(14);
  if (action === 0) { touch(i); tick(R.int(3000)); }
  else if (action === 1) { for (let k = 0; k < 1 + R.int(4); k++) request(i); }
  else if (action === 2) finish();
  else if (action === 3) consoleEntry(i);
  else if (action === 4) await report(i, R.bool(0.1) ? { origin: R.pick([SLOTS[i], 'https://evil.example']) } : {});
  else if (action === 5) await cliRead(R.pick(['network', 'console', 'server-log', 'slots', 'settings']));
  else if (action === 6) await call('PATCH', `/api/cli/preview/settings${qs(randQuery('settings'))}`, { ...tok, 'content-type': R.pick(['application/json', 'application/json', 'text/plain']) }, JSON.stringify(settingsPatch()));
  else if (action === 7 || action === 8) await mcp();
  else if (action === 9) await call(R.pick(['DELETE', 'GET']), `/api/cli/preview/clear${qs(randQuery('clear'))}`, tok);
  else if (action === 10) {
    const q = randQuery('pane');
    if (R.bool(0.1)) q.slot = SLOTS[0];
    await call(R.pick(['GET', 'GET', 'PATCH', 'DELETE', 'POST']), `/api/${P}/preview/devtools${qs(q)}${R.bool(0.05) ? '&slot=x' : ''}`, R.pick([{ cookie: COOKIE, 'content-type': 'application/json' }, { cookie: COOKIE, 'content-type': 'application/json' }, tok, {}]), JSON.stringify(settingsPatch()));
  } else if (action === 11) await call('GET', `/api/${P}/preview/devtools/server-log${qs(randQuery('server-log'))}`, R.pick([{ cookie: COOKIE }, { cookie: COOKIE }, tok]));
  else if (action === 12) tick(R.int(20_000));
  else await report(i, { many: 120 });
}
await report(0, { raw: '{"events":' });
await report(0, { raw: 'x'.repeat(256 * 1024 + 1) });
await report(0, { method: 'GET', path: '/__tk/devtools/config' });
await report(1, { method: 'POST', path: '/__tk/devtools/config' });
await report(0, { method: 'PUT' });
await call('GET', '/api/cli/preview/network?slot=a&slot=b', tok);
await call('GET', `/api/${P}/preview/devtools?slot=a&slot=b`, { cookie: COOKIE });
await call('GET', `/api/${P}/preview/devtools?since=1&since=2`, { cookie: COOKIE });
await call('PATCH', '/api/cli/preview/settings', { ...tok, 'content-type': 'application/json' }, '{broken');
await call('POST', '/api/cli/preview/slots', tok);

const target = process.env.GOLDEN_OUT ?? path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'devtools.json');
const scrub = (s) => neutral(s.split(data).join('$DATA'));
fs.writeFileSync(target, scrub(JSON.stringify({ secret: SECRET, token: TOKEN, start: FIXED, steps, panes: globalThis.__golden.panes, capture: globalThis.__golden.capture, listeners: globalThis.__golden.listeners }, null, 1)) + '\n');
console.log('wrote', target, steps.length, 'steps');
server.close();
process.exit(0);
