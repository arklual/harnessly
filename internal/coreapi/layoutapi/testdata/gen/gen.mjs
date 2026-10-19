// Sends a fixed list of requests to the previous runtime's own API dispatcher and handlers, on fixture files, and
// writes what each answered (status, the headers a caller acts on, body) to ../golden.json. The Go test sends the same
// requests to its routes on the same files and must answer byte for byte.
//
// The cli-token header the previous runtime reads is not spelled here: it comes from the caller, and golden.json names
// it NEUTRAL_TOKEN_HEADER, the header the Go test checks.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const FIXED = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'api-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const A = path.join(root, 'proj-a');
const B = path.join(root, 'proj-b');
for (const d of [A, B]) fs.mkdirSync(d);
const SID = '425055ec-bbc8-4e3c-87d0-36fa74c1cccb';
const TOKEN = 'a'.repeat(64);

globalThis.__golden = { tmux: { sessions: new Set(['tk-ws-a-pane-a-tab-1', 'tk-ws-a-pane-a-tab-2']) }, pasted: [] };

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const DATA = dataDir();

const fixtures = {
  'cli-token': TOKEN,
  'workspaces.json': JSON.stringify({
    workspaces: [{ id: 'ws-a', name: 'Alpha', directories: [A] }, { id: 'ws-b', name: 'Beta', directories: [B] }],
    groups: [], sidebarCollapsed: false, sidebarWidth: 240, updatedAt: '2026-01-01T00:00:00.000Z',
  }, null, 2),
  'workspaces/ws-a/layout.json': JSON.stringify({
    root: {
      type: 'pane', id: 'pane-a', activeTabId: 'tab-1',
      tabs: [
        { id: 'tab-1', sessionName: 'tk-ws-a-pane-a-tab-1', name: '', order: 0, cwd: A },
        { id: 'tab-2', sessionName: 'tk-ws-a-pane-a-tab-2', name: 'Claude', order: 1, panelType: 'claude-code', cliState: 'idle',
          agentState: { providerId: 'claude', sessionId: SID, jsonlPath: null, summary: null }, claudeSessionId: SID },
      ],
    },
    activePaneId: 'pane-a', updatedAt: '2026-01-01T00:00:00.000Z',
  }, null, 2),
};
for (const [name, content] of Object.entries(fixtures)) {
  fs.mkdirSync(path.dirname(path.join(DATA, name)), { recursive: true });
  fs.writeFileSync(path.join(DATA, name), content, { mode: 0o600 });
}

const routes = [
  '/api/layout', '/api/layout/pane', '/api/layout/pane/[paneId]', '/api/layout/pane/[paneId]/tabs',
  '/api/layout/pane/[paneId]/tabs/order', '/api/layout/pane/[paneId]/tabs/[tabId]', '/api/layout/pane/[paneId]/tabs/[tabId]/move',
  '/api/workspace', '/api/workspace/[workspaceId]', '/api/workspace/active', '/api/workspace/reorder', '/api/workspace/group',
  '/api/workspace/group/[groupId]', '/api/workspace/group/reorder',
  '/api/cli/workspaces', '/api/cli/tabs', '/api/cli/tabs/[tabId]', '/api/cli/tabs/[tabId]/result', '/api/cli/tabs/[tabId]/send',
  '/api/cli/tabs/[tabId]/status',
].map((route) => ({ route, load: () => import(`@/pages${route}`) }));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const OLD_HEADER = process.env.OLD_RUNTIME_TOKEN_HEADER;
if (!OLD_HEADER) throw new Error('OLD_RUNTIME_TOKEN_HEADER is required');
const NEUTRAL_TOKEN_HEADER = 'x-cli-token';
const tok = { [OLD_HEADER]: TOKEN };
const neutral = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [k === OLD_HEADER ? NEUTRAL_TOKEN_HEADER : k, v]));
const ses = { cookie: 'session-token=good' };
const json = { 'content-type': 'application/json' };
const requests = [
  ['GET', '/api/workspace', {}],
  ['GET', '/api/cli/workspaces', {}],
  ['GET', '/api/cli/workspaces', tok],
  ['GET', '/api/cli/workspaces', { ...tok, 'if-none-match': 'ETAG' }],
  ['POST', '/api/cli/workspaces', tok],
  ['GET', '/api/workspace', ses],
  ['GET', '/agents/api/workspace/', tok],
  ['GET', '/api/cli/tabs?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs', tok],
  ['GET', '/api/cli/tabs/tab-2?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs/tab-x?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs/tab-2', tok],
  ['GET', '/api/cli/tabs/tab-2/status?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs/tab-1/status?workspaceId=ws-b', tok],
  ['GET', '/api/cli/tabs/tab-1/result?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs/tab-1/send?workspaceId=ws-a', tok],
  ['POST', '/api/cli/tabs/tab-1/send?workspaceId=ws-a', { ...tok, ...json }, '{"content":""}'],
  ['POST', '/api/cli/tabs/tab-1/send?workspaceId=ws-a', { ...tok, ...json }, '{"content":"echo \\"hi\\""}'],
  ['POST', '/api/cli/tabs', { ...tok, ...json }, '{"workspaceId":"ws-a","panelType":"x"}'],
  ['POST', '/api/cli/tabs', { ...tok, ...json }, '{"workspaceId":"ws-a","prompt":"go"}'],
  ['POST', '/api/cli/tabs', { ...tok, ...json }, '{"workspaceId":"ws-a","name":" Shell "}'],
  ['DELETE', '/api/cli/tabs/tab-1?workspaceId=ws-a', tok],
  ['GET', '/api/cli/tabs?workspaceId=ws-a', tok],
  ['PATCH', '/api/workspace/active', { ...ses, ...json }, '{"sidebarWidth":'],
  ['PATCH', '/api/workspace/active', { ...ses, ...json }, `{"x":"${'a'.repeat(1 << 20)}"}`],
  ['PATCH', '/api/workspace/active', { ...ses, ...json }, '{"sidebarWidth":300}'],
  ['GET', '/api/layout?workspace=ws-a', ses],
  ['PATCH', '/api/layout?workspace=ws-a', { ...ses, ...json }, '{"activePaneId":"nope"}'],
  ['PATCH', '/api/layout/pane/pane-a/tabs/order?workspace=ws-a', { ...ses, ...json }, '{"tabIds":[]}'],
  ['PUT', '/api/layout/pane/pane-a/tabs/tab-2?workspace=ws-a', ses],
  ['PATCH', '/api/layout/pane/pane-a/tabs/tab-2?workspace=ws-a', { ...ses, ...json }, '{"name":" Renamed ","terminalRatio":140}'],
  ['PATCH', '/api/workspace/ws-a', { ...ses, ...json }, '{"name":" Alpha 2 "}'],
  ['POST', '/api/workspace/group', { ...ses, ...json }, '{}'],
  ['GET', '/api/workspace', ses],
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const scrub = (s) => s.split(root).join('$ROOT');
const out = [];
let lastEtag = '';
for (const [method, url, headers, body] of requests) {
  const h = { ...headers };
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  // node's own client: fetch would add Cache-Control: no-cache to a conditional request.
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: body === undefined ? h : { ...h, 'content-length': Buffer.byteLength(body) } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end(body);
  });
  const text = res.text;
  if (res.headers.etag) lastEtag = res.headers.etag;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  out.push({
    method, url, headers: neutral(headers),
    body: body && body.length > 100000 ? { repeat: body.length } : body ?? null,
    status: res.status, answerHeaders: kept, answer: scrub(text),
  });
}
server.close();
const files = {};
for (const name of ['workspaces.json', 'workspaces/ws-a/layout.json', 'workspaces/ws-b/layout.json']) {
  files[name] = fs.existsSync(path.join(DATA, name)) ? scrub(fs.readFileSync(path.join(DATA, name), 'utf8')) : null;
}
const scrubbed = Object.fromEntries(Object.entries(fixtures).map(([k, v]) => [k, scrub(v)]));
const target = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'golden.json');
fs.writeFileSync(target, JSON.stringify({ token: TOKEN, fixtures: scrubbed, requests: out, files, pasted: globalThis.__golden.pasted }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests → ${target}`);
