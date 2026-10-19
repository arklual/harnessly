// Runs the previous runtime's own design library code on a fixture library and writes what it answered to
// ../golden.json: its API routes through its dispatcher (the listing, board edits, locate, send, message, continue,
// the CLI's design, claims), its library server's answers, and the pure functions on generated inputs. The Go tests
// run the same requests on the same files and must answer byte for byte.
//
// The previous runtime spells the product in module names, a header, its CLI's name and a few texts; those words come
// from the environment and are replaced with neutral ones in golden.json.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_MODULE_PREFIX=<its module word> \
//   OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> OLD_RUNTIME_CLI=<its CLI's name> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// GEN_RANDOM=<n> GEN_OUT=<file> writes n generated cases of the pure functions instead (a large set, not committed).
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';

const P = process.env.OLD_RUNTIME_MODULE_PREFIX;
const OLD_HEADER = process.env.OLD_RUNTIME_TOKEN_HEADER;
const CLI = process.env.OLD_RUNTIME_CLI;
if (!P || !OLD_HEADER || !CLI) throw new Error('OLD_RUNTIME_MODULE_PREFIX, OLD_RUNTIME_TOKEN_HEADER and OLD_RUNTIME_CLI are required');
const UP = P.toUpperCase();
const Cap = P[0].toUpperCase() + P.slice(1);
const NEUTRAL = { prefix: 'acme', Prefix: 'Acme', PREFIX: 'ACME', cli: 'acme-cli', tokenHeader: 'x-cli-token', api: '/api/platform' };

const FIXED = Date.parse('2026-10-18T09:30:00.000Z');
const NOW = { t: FIXED };
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(NOW.t); }
  static now() { return NOW.t; }
};
os.userInfo = () => ({ username: 'agent', uid: 1000, gid: 1000, shell: '/bin/sh', homedir: '/home/agent' });

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'design-golden-')));
const home = path.join(root, 'home');
const DATA = path.join(root, 'data');
const WS = path.join(root, 'ws');
const LIB = path.join(WS, 'design-artifacts');
for (const d of [home, DATA, LIB]) fs.mkdirSync(d, { recursive: true });
process.env.HOME = home;
process.env[`${UP}_AI_DIR`] = DATA;
process.env[`${UP}_WORKSPACE_DIR`] = WS;
process.env[`${UP}_DESIGN_LIBRARY_PORT`] = '0';

const scrub = (s) => s.split(root).join('$ROOT').split(`/api/${P}/`).join(`${NEUTRAL.api}/`).split(CLI).join(NEUTRAL.cli)
  .split(UP).join(NEUTRAL.PREFIX).split(Cap).join(NEUTRAL.Prefix).split(P).join(NEUTRAL.prefix);
const etagOf = (payload) => `"${crypto.createHash('sha1').update(payload).digest('base64url').slice(0, 27)}"`;

const M = (name) => import(`@/lib/${P}-design-library/${name}`);

if (process.env.GEN_RANDOM) {
  const { units } = await import('./units.mjs');
  const cases = await units(Number(process.env.GEN_RANDOM), { M, scrub, LIB });
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify(cases));
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  fs.rmSync(root, { recursive: true, force: true });
  process.exit(0);
}

const TOKEN = 'a'.repeat(64);
fs.writeFileSync(path.join(DATA, 'cli-token'), TOKEN, { mode: 0o600 });
fs.writeFileSync(path.join(DATA, 'workspaces.json'), JSON.stringify({ workspaces: [{ id: 'ws-a', name: 'Alpha' }] }));

// The fixture library: written, then every file and folder dated so the answers do not depend on when it ran.
const files = {
  'main-site/artifact.json': JSON.stringify({ v: 1, title: '  Главный сайт ', source: 'export.zip', extra: { keep: true }, boards: [
    { title: 'Главная', path: 'Main.dc.html', w: 1440.4, h: 900 },
    { title: 'Ушедшая', path: 'Gone.dc.html' },
    { title: 'Страница', path: 'lib/page.html?id=hub&v=Desktop&theme=dark' },
    { path: 'Untitled.dc.html', w: 0, h: 30000 },
    { title: 'bad', path: '../x.html' },
    'junk',
  ] }, null, 2),
  'main-site/project/Main.dc.html': '<!doctype html><html><head><meta charset="utf-8"><script src="./support.js"></script><link rel="stylesheet" href="styles.css?x=1#top"></head><body><img src=\'img/logo.png\'><script src="https://cdn.example/x.js"></script><img src="{{ hole }}"><a href="styles.css">x</a><main class="board">Привет, мир большой и длинный текст доски</main></body></html>\n',
  'main-site/project/Untitled.dc.html': '<html><body>u</body></html>',
  'main-site/project/Extra.dc.html': '<html><head></head><body>extra</body></html>',
  'main-site/project/Wrapper.dc.html': "<x-dc><script>renderVals(){ return { src: 'lib/page.html?id=pay&v=' + (props.variant ?? 'Mobile') } }</script></x-dc>",
  'main-site/project/_board-template.html': '<html></html>',
  'main-site/project/support.js': 'window.support = 1;\n',
  'main-site/project/styles.css': `body{margin:0}\n${'.a{color:red}\n'.repeat(120)}`,
  'main-site/project/img/logo.png': Buffer.from('89504e470d0a1a0a0000000d49484452', 'hex').toString('latin1'),
  'main-site/project/lib/page.html': '<html><head><title>page</title></head><body>page</body></html>',
  'main-site/project/fonts/a.woff2': 'font',
  'main-site/project/node_modules/x/index.js': 'x',
  'main-site/project/canvas.json': JSON.stringify({ boards: { 'Main.dc.html': { x: 0, y: 0, w: 1280, h: 720 }, 'Untitled.dc.html': { w: 390, h: 844 } } }),
  'canvas-only/project/canvas.json': JSON.stringify({ title: 'Холст', order: ['B.dc.html', 'A.dc.html', 'A.dc.html', 'sub/C.dc.html', 'Nope.dc.html', 'x.html'], boards: { 'A.dc.html': { title: 'Альфа', x: 10, w: 800 }, 'B.dc.html': { y: 5, h: 600 }, 'Nope.dc.html': 7 } }),
  'canvas-only/project/A.dc.html': '<html>a</html>',
  'canvas-only/project/B.dc.html': '<html>b</html>',
  'files-only/project/Доска 10.dc.html': 'x',
  'files-only/project/Доска 2.dc.html': 'x',
  'files-only/project/Все страницы.dc.html': 'x',
  'files-only/project/Главная.dc.html': 'x',
  'files-only/project/alpha.dc.html': 'x',
  'files-only/project/Zeta.dc.html': 'x',
  'files-only/project/notes.txt': 'x',
  'wrapped/artifact.json': JSON.stringify({ title: 'Обёртки', boards: [{ title: 'Выплаты', path: 'lib/page.html?id=payouts&v=Desktop&theme=dark' }] }),
  'wrapped/project/Выплаты.dc.html': `<x-dc><script type="text/x-dc" data-dc-script data-props='{"variant":{"options":["Desktop","Empty","bad value",3]},"theme":{"default":"dark"}}'>renderVals(){ return 'lib/page.html?id=payouts&v=' }</script></x-dc>`,
  'wrapped/project/lib/page.html': '<html><head></head><body>p</body></html>',
  'no-project/artifact.json': '{}',
  'Bad_ID/project/A.dc.html': 'x',
  '.hidden/project/A.dc.html': 'x',
  'stray-file': 'x',
};
for (const [name, content] of Object.entries(files)) {
  const file = path.join(LIB, name);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content, name.endsWith('.png') ? 'latin1' : 'utf8');
}
fs.symlinkSync(path.join(LIB, 'files-only'), path.join(LIB, 'linked'));
const STAMP = FIXED / 1000 - 3600;
const dates = {};
const date = (dir, depth) => {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isSymbolicLink()) continue;
    if (entry.isDirectory()) date(full, depth + 1);
    const rel = path.relative(LIB, full);
    const t = STAMP - (rel.length % 7) * 600 + depth * 60;
    dates[rel] = t;
    fs.utimesSync(full, t, t);
  }
};
date(LIB, 0);

// Claims on disk: one live of another runtime, one stale, one of this runtime's chat.
const claimsDir = path.join(LIB, '.claims', 'main-site');
fs.mkdirSync(claimsDir, { recursive: true });
const { claimFileName } = await M('claims');
const writeClaim = (claim) => fs.writeFileSync(path.join(claimsDir, claimFileName(claim.board)), `${JSON.stringify(claim, null, 2)}\n`);
writeClaim({ project: 'main-site', board: 'Main', owner: { user: 'Bob', runtime: 'other', tabId: 'tab-x', chatTitle: 'Чужой чат' }, since: FIXED - 60_000, heartbeatAt: FIXED - 10_000 });
writeClaim({ project: 'main-site', board: 'Extra', owner: { user: 'Old', runtime: 'other', tabId: 'tab-y', chatTitle: 'Старый' }, since: FIXED - 900_000, heartbeatAt: FIXED - 600_000 });
writeClaim({ project: 'main-site', board: 'Untitled', owner: { user: 'ann', runtime: 'agent', tabId: 'tab-2', chatTitle: 'Мой чат' }, since: FIXED - 30_000, heartbeatAt: FIXED - 5_000 });

const claimFiles = Object.fromEntries(fs.readdirSync(claimsDir).map((n) => [n, fs.readFileSync(path.join(claimsDir, n), 'utf8')]));

// A transcript of the agent in tab-2 that read the library and edited a board.
const projects = path.join(home, '.claude', 'projects', '-ws');
fs.mkdirSync(projects, { recursive: true });
const transcript = path.join(projects, 'sess.jsonl');
const line = (o) => `${JSON.stringify(o)}\n`;
fs.writeFileSync(transcript, [
  line({ type: 'user', uuid: 'u1', timestamp: '2026-10-18T09:20:00.000Z', message: { role: 'user', content: 'поправь' }, cwd: WS }),
  line({ type: 'assistant', uuid: 'a1', timestamp: '2026-10-18T09:25:00.000Z', message: { role: 'assistant', id: 'm1', content: [
    { type: 'tool_use', id: 't1', name: 'Bash', input: { command: `ls ${LIB}/files-only/project` } },
  ] } }),
  line({ type: 'assistant', uuid: 'a2', timestamp: '2026-10-18T09:26:00.000Z', message: { role: 'assistant', id: 'm2', content: [
    { type: 'tool_use', id: 't2', name: 'Edit', input: { file_path: `${LIB}/main-site/project/Extra.dc.html`, old_string: 'a', new_string: 'b' } },
  ] } }),
].join(''));

globalThis.__golden = {
  ownerEmail: 'ann@example.com',
  clients: {
    'tab-1': { panelType: 'terminal', cliState: 'idle', tabName: 'Shell', workspaceId: 'ws-a', lastEvent: null },
    'tab-2': { panelType: 'claude-code', cliState: 'busy', agentSummary: 'Правки досок', tabName: 'Claude', workspaceId: 'ws-a', lastEvent: { name: 'x', at: FIXED - 1000, seq: 3 } },
    'tab-3': { panelType: 'codex-cli', cliState: 'idle', agentSummary: '', tabName: '', workspaceId: 'ws-z', lastEvent: null },
  },
  sources: [
    { tabId: 'tab-2', tmuxSession: 'hs-ws-a-tab-2', jsonlPath: transcript },
    { tabId: 'tab-3', tmuxSession: 'hs-ws-a-tab-3', jsonlPath: null },
  ],
  workspaces: [{ id: 'ws-a', name: 'Alpha' }],
  layoutTabs: {
    'tab-2': { tab: { panelType: 'claude-code', sessionName: 'hs-ws-a-tab-2' } },
    'tab-3': { tab: { panelType: 'codex-cli', sessionName: 'hs-ws-a-tab-3' } },
    'tab-1': { tab: { panelType: 'terminal', sessionName: 'hs-ws-a-tab-1' } },
  },
  fronts: { 'hs-ws-a-tab-2': { isSafe: false, processName: 'claude' }, 'hs-ws-a-tab-3': { isSafe: false, processName: 'codex' } },
  typed: [], subscriptions: [], removed: [], pushed: [], statuses: {}, started: [],
};

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const routes = [
  `/api/${P}/design/library`, `/api/${P}/design/library/boards`, `/api/${P}/design/library/locate`, `/api/${P}/design/library/send`,
  `/api/${P}/design/library/message`, `/api/${P}/design/library/continue`, '/api/cli/design', '/api/cli/design/claims',
  '/api/cli/design/history',
].map((route) => ({ route, load: () => import(`@/pages${route}`) }));
const dispatch = createApiDispatcher(routes);
const api = http.createServer((req, res) => { dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } }); });
await new Promise((r) => api.listen(0, '127.0.0.1', r));
const { handleLibraryRequest } = await M('server');
const LIB_PORT = 18470;
const lib = http.createServer((req, res) => { handleLibraryRequest(req, res, LIB_PORT).catch(() => { res.statusCode = 500; res.end(); }); });
await new Promise((r) => lib.listen(0, '127.0.0.1', r));

// GEN_SERVE=<file>: the API stays up on the fixture for a CLI to talk to; its port goes to the file.
if (process.env.GEN_SERVE) {
  fs.writeFileSync(process.env.GEN_SERVE, String(api.address().port));
  await new Promise((r) => process.on('SIGTERM', r));
  process.exit(0);
}

const tok = { [OLD_HEADER]: TOKEN };
const ses = { cookie: 'session-token=good' };
const json = { 'content-type': 'application/json' };
const A = `/api/${P}/design/library`;
const libHost = { host: `localhost:${LIB_PORT}` };
const same = { ...libHost, 'sec-fetch-site': 'same-origin' };

// [server, method, url, headers, body, at]: "api" or "lib"; at moves the clock first.
const requests = [
  ['api', 'GET', A, {}],
  ['api', 'GET', A, tok],
  ['api', 'GET', `${A}?lite=1&tab=tab-2`, ses],
  ['api', 'GET', `${A}?tab=tab-2`, ses],
  ['api', 'GET', `${A}?tab=bad%20id`, { ...ses, 'if-none-match': 'ETAG' }],
  ['api', 'POST', A, { ...ses, ...json }, '{}'],
  ['api', 'GET', '/api/cli/design', {}],
  ['api', 'GET', '/api/cli/design', tok],
  ['api', 'GET', '/api/cli/design?logs=main-site', tok],
  ['api', 'GET', '/api/cli/design?logs=Bad', tok],
  ['api', 'GET', '/api/cli/design?state=main-site', tok],
  ['api', 'PUT', '/api/cli/design', tok],
  ['api', 'POST', '/api/cli/design', { ...tok, ...json }, '{"artifact":"nope!"}'],
  ['api', 'POST', '/api/cli/design', { ...tok, ...json }, '{"artifact":"main-site","control":{"x":1}}'],
  ['api', 'POST', '/api/cli/design', { ...tok, ...json }, '{"artifact":"main-site","control":{"board":"Main","view":"phone"}}'],
  ['api', 'POST', '/api/cli/design', { ...tok, ...json }, '{"artifact":'],
  ['lib', 'GET', '/', libHost],
  ['lib', 'GET', '/', { host: 'evil.example' }],
  ['lib', 'POST', '/', libHost],
  ['lib', 'HEAD', '/a/main-site/', libHost],
  ['lib', 'GET', '/a/main-site', libHost],
  ['lib', 'GET', '/a/main-site/', libHost],
  ['lib', 'GET', '/a/files-only/', libHost],
  ['lib', 'GET', '/a/nope/', libHost],
  ['lib', 'GET', '/a/main-site/x', libHost],
  ['lib', 'GET', '/a/main-site/p/Main.dc.html', libHost],
  ['lib', 'GET', '/a/main-site/p/lib/page.html?id=hub', libHost],
  ['lib', 'GET', '/a/main-site/p/styles.css', { ...libHost, 'accept-encoding': 'identity' }],
  ['lib', 'GET', '/a/main-site/p/styles.css', { ...libHost, 'accept-encoding': 'br' }],
  ['lib', 'GET', '/a/main-site/p/styles.css', libHost],
  ['lib', 'GET', '/a/main-site/p/support.js', { ...libHost, 'if-none-match': 'ETAG' }],
  ['lib', 'GET', '/a/main-site/p/support.js?tkv=VERSION', libHost],
  ['lib', 'GET', '/a/main-site/p/img/logo.png', libHost],
  ['lib', 'GET', '/a/main-site/p/fonts/a.woff2', libHost],
  ['lib', 'GET', '/a/main-site/p/../artifact.json', libHost],
  ['lib', 'GET', '/a/main-site/p/notes.md', libHost],
  ['lib', 'GET', '/a/main-site/p/%E0%A4%A', libHost],
  ['lib', 'GET', '/favicon.ico', libHost],
  ['lib', 'GET', '/__lib/version?a=main-site', libHost],
  ['lib', 'GET', '/__lib/version?a=nope', libHost],
  ['lib', 'GET', '/__lib/boards?a=wrapped', libHost],
  ['lib', 'GET', '/__lib/boards?a=main-site', libHost],
  ['lib', 'GET', '/__lib/changes?a=main-site&since=0', libHost],
  ['lib', 'GET', '/__lib/changes?a=main-site', libHost],
  ['lib', 'GET', '/__lib/changes?a=main-site&since=abc', libHost],
  ['lib', 'GET', '/__lib/log?a=main-site&k=error&p=Main.dc.html&m=boom%01x', libHost],
  ['lib', 'GET', '/__lib/log?a=main-site&k=error&p=Main.dc.html&m=boom%01x', same],
  ['lib', 'GET', '/__lib/log?a=main-site&k=nope&m=x', same],
  ['lib', 'GET', '/__lib/state?a=main-site&s=%7B%22board%22%3A%22Main%22%7D', same],
  ['lib', 'GET', '/__lib/state?a=main-site&s=%7Bbad', same],
  ['api', 'GET', '/api/cli/design?logs=main-site', tok],
  ['api', 'GET', '/api/cli/design?state=main-site', tok],
  ['api', 'POST', `${A}/locate`, { ...ses, ...json }, '{"path":"/a/main-site/p/Main.dc.html","text":"Привет,   мир большой"}'],
  ['api', 'POST', `${A}/locate`, { ...ses, ...json }, '{"path":"/a/main-site/p/Main.dc.html","text":"нет такого текста"}'],
  ['api', 'POST', `${A}/locate`, { ...ses, ...json }, '{"path":"/a/nope/p/x.html"}'],
  ['api', 'POST', `${A}/locate`, { ...ses, ...json }, '{"path":7}'],
  ['api', 'POST', `${A}/locate`, ses, '{"path":"x"}'],
  ['api', 'GET', `${A}/locate`, ses],
  ['api', 'POST', `${A}/locate`, { ...json }, '{"path":"x"}'],
  ['api', 'POST', `${A}/locate`, { ...tok, ...json }, '{"path":"x"}'],
  ['api', 'GET', '/api/cli/design/claims', tok],
  ['api', 'GET', '/api/cli/design/claims?project=nope', tok],
  ['api', 'GET', '/api/cli/design/claims?project=main-site', tok],
  ['api', 'DELETE', '/api/cli/design/claims', tok],
  ['api', 'POST', '/api/cli/design/claims', tok, '{}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-2"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-9","project":"main-site","board":"Main"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-2","project":"main-site","board":"Главная"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-2","project":"main-site","board":"Extra.dc.html"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-2","project":"main-site","board":"Новая доска"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-3","project":"main-site","board":"Extra"}', FIXED + 5_000],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"claim","session":"hs-ws-a-tab-2","project":"main-site","board":"a/b"}'],
  ['api', 'GET', '/api/cli/design/claims', tok],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"release","session":"hs-ws-a-tab-2","board":"x"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"release","session":"hs-ws-a-tab-2","project":"main-site","board":"Extra"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"release","session":"hs-ws-a-tab-2"}'],
  ['api', 'POST', '/api/cli/design/claims', { ...tok, ...json }, '{"action":"nope","session":"hs-ws-a-tab-2"}'],
  ['api', 'GET', `${A}?tab=tab-3`, ses],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"bogus"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"rename","path":3}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"rename","path":"Zeta.dc.html"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"rename","path":"Zeta.dc.html","title":"  Зета\\u0001  новая "}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"move","path":"Zeta.dc.html","delta":-5}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"move","path":"alpha.dc.html","delta":1}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"duplicate","path":"alpha.dc.html"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"duplicate","path":"alpha.dc.html"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"remove","path":"Главная.dc.html"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"remove","path":"nope.dc.html"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"files-only","action":"title","title":"Файлы"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"wrapped","action":"duplicate","path":"lib/page.html?id=payouts&v=Desktop&theme=dark"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"wrapped","action":"remove","path":"lib/page.html?id=payouts&v=Desktop&theme=dark"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"Bad","action":"title","title":"x"}'],
  ['api', 'POST', `${A}/boards`, { ...ses, ...json }, '{"artifact":"gone-away","action":"title","title":"x"}'],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"../x.html"}'],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"Main.dc.html"}'],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"nope","board":"Main.dc.html"}'],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"Main.dc.html","title":null}'],
  ['setSubs', [{ endpoint: 'https://push.example/a', keys: {}, deviceId: 'd1', mobile: true }, { endpoint: 'https://push.example/b', keys: {}, deviceId: 'd2' }, { endpoint: 'https://push.example/c', keys: {}, deviceId: 'd3', mobile: true }], { 'https://push.example/c': 410 }],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"Main.dc.html?x=1","deviceId":"d1"}'],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"lib/page.html?id=hub&v=Desktop&theme=dark","title":"  Своё   имя  "}'],
  ['setSubs', [{ endpoint: 'https://push.example/c', keys: {}, mobile: true }], { 'https://push.example/c': 404 }],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"Main.dc.html"}'],
  ['setSubs', [{ endpoint: 'https://push.example/e', keys: {} }], { 'https://push.example/e': 500 }],
  ['api', 'POST', `${A}/send`, { ...ses, ...json }, '{"artifact":"main-site","board":"Main.dc.html"}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-2","artifact":"main-site","text":"   "}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-9","artifact":"main-site","text":"hi"}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-1","artifact":"main-site","text":"hi"}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-2","artifact":"nope","text":"hi"}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-3","artifact":"main-site","text":"hi"}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-2","artifact":"main-site","text":"поправь \\u001b[31mэто\\\\\\n  "}'],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, `{"tabId":"tab-2","artifact":"main-site","text":"${'длинно '.repeat(40)}"}`],
  ['api', 'POST', `${A}/message`, { ...ses, ...json }, '{"tabId":"tab-2","artifact":"main-site","text":"<annotation>\\nline\\u0007\\n</annotation>","blocks":true}'],
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length', 'content-encoding', 'last-modified', 'vary',
  'x-content-type-options', 'referrer-policy'];
const neutralHeaders = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [k === OLD_HEADER ? NEUTRAL.tokenHeader : k, v]));
const out = [];
let lastEtag = '';
let supportVersion = '';
for (const item of requests) {
  if (item[0] === 'setSubs') {
    globalThis.__golden.subscriptions = item[1];
    globalThis.__golden.statuses = item[2];
    out.push({ setSubs: item[1], statuses: item[2] });
    continue;
  }
  const [which, method, rawUrl, headers, body, at] = item;
  if (at) NOW.t = at;
  const url = rawUrl.replace('VERSION', supportVersion);
  const h = { ...headers };
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const base = `http://127.0.0.1:${(which === 'api' ? api : lib).address().port}`;
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: body === undefined ? h : { ...h, 'content-length': Buffer.byteLength(body) } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, raw: Buffer.concat(chunks) }));
    });
    req.on('error', reject);
    req.end(body);
  });
  let raw = res.raw;
  if (res.headers['content-encoding'] === 'gzip') raw = zlib.gunzipSync(raw);
  if (res.headers.etag) lastEtag = res.headers.etag;
  if (url.endsWith('support.js') && res.headers.etag) supportVersion = res.headers.etag.slice(3, -1);
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  let answer = scrub(raw.toString('utf8'));
  // The library server's port differs between the two runtimes.
  answer = answer.replace(/"port":\d+/g, '"port":0');
  // An ETag of the API is over the bytes as answered; after the neutral words go in, it is over the bytes in golden.json.
  if (which === 'api' && kept.etag) kept.etag = etagOf(answer);
  if (answer !== raw.toString('utf8')) delete kept['content-length'];
  if (res.headers['content-encoding'] === 'gzip') delete kept['content-length'];
  out.push({ server: which, method, url: scrub(url), headers: neutralHeaders(headers), body: body ?? null, at: at ?? null, status: res.status,
    answerHeaders: kept, answer });
}
api.close();
lib.close();

const tree = {};
const walk = (dir) => {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    const rel = path.relative(LIB, full);
    if (entry.isSymbolicLink()) { tree[rel] = 'link'; continue; }
    if (entry.isDirectory()) { walk(full); continue; }
    if (rel.includes('node_modules')) continue;
    tree[scrub(rel)] = scrub(fs.readFileSync(full, 'utf8'));
  }
};
walk(LIB);
const dataFiles = {};
for (const f of ['design/library/main-site.md']) {
  const full = path.join(DATA, f);
  if (fs.existsSync(full)) dataFiles[f] = scrub(fs.readFileSync(full, 'utf8'));
}
const scrubbedFiles = Object.fromEntries(Object.entries(files).map(([k, v]) => [k, scrub(v)]));
const g = globalThis.__golden;
const target = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'golden.json');
fs.writeFileSync(target, JSON.stringify({
  now: FIXED, token: TOKEN, files: scrubbedFiles, dates, claimFiles,
  clients: g.clients, sources: g.sources.map((s) => ({ ...s, jsonlPath: s.jsonlPath ? scrub(s.jsonlPath) : null })),
  transcript: scrub(fs.readFileSync(transcript, 'utf8')),
  requests: out, tree, dataFiles,
  typed: JSON.parse(scrub(JSON.stringify(g.typed))), pushed: JSON.parse(scrub(JSON.stringify(g.pushed))), removed: g.removed,
}, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests → ${target}`);
process.exit(0);
