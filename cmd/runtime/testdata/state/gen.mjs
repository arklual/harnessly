// Sends a fixed list of requests to the previous runtime's own API dispatcher and the handlers of the state slice
// (settings files, automations, sign-in, telemetry, the dead routes) and of the methods and queries the other slices
// now answer, on fixture folders, and writes what each answered and the data folder after it to ../state-golden.json.
// The Go test lays out the same, replays the requests on its routes and must match.
//
// The previous runtime's product names are not spelled here: the caller passes them, and the output is neutral.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> OLD_RUNTIME_PLATFORM_API=<its platform prefix> \
//   OLD_DATA_DIR_VAR=<its data folder variable> OLD_WORKSPACE_VAR=<its workspace folder variable> \
//   OLD_OWNER_VAR=<its owner e-mail variable> OLD_LAYOUT_KEY=<its config key of the column widths> \
//   OLD_DEFAULTS_KEY=<its config key of the launch defaults> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import { syncBuiltinESMExports } from 'node:module';
import os from 'node:os';
import path from 'node:path';

const NEEDED = ['OLD_RUNTIME_TOKEN_HEADERS', 'OLD_RUNTIME_PLATFORM_API', 'OLD_DATA_DIR_VAR', 'OLD_WORKSPACE_VAR', 'OLD_OWNER_VAR',
  'OLD_LAYOUT_KEY', 'OLD_DEFAULTS_KEY'];
for (const k of NEEDED) if (!process.env[k]) throw new Error(`${k} is required`);
const OLD_TOKENS = process.env.OLD_RUNTIME_TOKEN_HEADERS.split(',');
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
const NEUTRAL_TOKENS = ['x-cli-token', 'x-old-cli-token'];
// Neutral words of the requests and their old spelling; answers and files are written back the other way.
const WORDS = [['acmeLayout', process.env.OLD_LAYOUT_KEY], ['acmeLaunchDefaults', process.env.OLD_DEFAULTS_KEY]];
const here = path.dirname(new URL(import.meta.url).pathname);

// Each request runs at its own second, so a write shows when it happened.
const FIXED = Date.parse('2026-10-19T09:00:00.000Z');
let clock = FIXED;
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(clock); }
  static now() { return clock; }
};
// Random bytes are a counter in the last four, as in the Go test.
let draws = 0;
crypto.randomBytes = (n) => { draws += 1; const b = Buffer.alloc(n); b.writeUInt32BE(draws, n - 4); return b; };
syncBuiltinESMExports();

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'state-golden-')));
const home = path.join(root, 'home');
const data = path.join(root, 'data');
const work = path.join(root, 'work');
for (const d of [home, data, work]) fs.mkdirSync(d, { recursive: true });
process.env.HOME = home;
process.env.PORT = '18099';
process.env.PATH = `${path.join(root, 'nobin')}:/usr/bin:/bin`;
process.env[process.env.OLD_DATA_DIR_VAR] = data;
process.env[process.env.OLD_WORKSPACE_VAR] = work;
for (const k of ['HOST', 'INIT_PASSWORD', 'AUTH_PASSWORD', 'DATABASE_URL', 'PGHOST', process.env.OLD_OWNER_VAR]) delete process.env[k];
process.chdir(root);
const TOKEN = 'c'.repeat(64);
fs.writeFileSync(path.join(data, 'cli-token'), TOKEN, { mode: 0o600 });

// --- fixtures -----------------------------------------------------------------------------------------------------------
const fixtures = [
  { path: 'work/a.png', text: 'png' },
  { path: 'work/notes.md', text: '# notes\n' },
  { path: 'work/data.bin', text: 'bin' },
  { path: 'home/.claude/projects/p/memory/fact.md', text: '---\nname: Fact\ndescription: a fact\n---\nbody\n' },
  { path: 'data/stats/daily-reports/2026-10-01.json', text: '{"date":"2026-10-01","brief":"b1","cost":1}' },
  { path: 'data/stats/daily-reports/2026-10-02.json', text: '{"date":"2026-10-02"}' },
  { path: 'data/stats/daily-reports/broken.json', text: '{' },
  { path: 'data/stats/daily-reports/notes.txt', text: 'x' },
  { path: 'bin/tailscale', text: '#!/bin/sh\ncase "$1 $2" in\n  "version "*) echo "1.80.2"; echo "  go1.24";;\n  "status --json") echo \'{"BackendState":"Running","Self":{"HostName":"box","DNSName":"box.tail.ts.net.","TailscaleIPs":["100.64.0.7","fd7a::7"]}}\';;\n  "serve status") echo \'{"Web":{"box.tail.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8022"}}},"box.tail.ts.net:8443":{"Handlers":{"/x":{"Proxy":"y"}}}}}\';;\n  "serve --bg") exit 0;;\n  "serve off") echo "no such serve" >&2; exit 3;;\nesac\n', mode: 0o755 },
];
const lay = (base) => {
  for (const f of fixtures) {
    const p = path.join(base, f.path);
    fs.mkdirSync(path.dirname(p), { recursive: true });
    fs.writeFileSync(p, f.text, { mode: f.mode ?? 0o644 });
  }
};
lay(root);
// The repository a diff fetches into: the same script in the Go test gives the same commits.
const GIT_SETUP = `set -e
export GIT_AUTHOR_NAME=A GIT_AUTHOR_EMAIL=a@example.com GIT_COMMITTER_NAME=A GIT_COMMITTER_EMAIL=a@example.com
export GIT_AUTHOR_DATE=2026-10-19T09:00:00Z GIT_COMMITTER_DATE=2026-10-19T09:00:00Z
g() { git -c init.defaultBranch=main -c advice.detachedHead=false "$@"; }
cd "$ROOT/work"
g init -q --bare remote.git
g clone -q remote.git seed 2>/dev/null
(cd seed && echo one > f && g add f && g commit -qm one && g push -q origin main)
rm -rf repo && g clone -q remote.git repo
(cd seed && echo two >> f && g commit -qam two && g push -q origin main)
g clone -q remote.git broken && (cd broken && g remote set-url origin "$ROOT/missing.git")
`;
execFileSync('sh', ['-c', GIT_SETUP], { env: { ...process.env, ROOT: root, GIT_CONFIG_NOSYSTEM: '1' }, stdio: 'ignore' });
globalThis.__golden = { sessions: { 'tab-1': path.join(work, 'repo'), 'tab-plain': work, 'tab-broken': path.join(work, 'broken') } };

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const P = OLD_API.slice(5); // the platform's folder under pages/api
const files = [
  'config', 'keybindings', 'message-history', 'quick-prompts', 'sidebar-items',
  `${P}/automations/rules/index`, `${P}/automations/rules/[id]`, `${P}/automations/runs/index`, `${P}/automations/token`,
  `${P}/net-events`, `${P}/gateway-ping`, 'stats/daily-report/cache', 'react-scan',
  'auth/login', 'auth/logout', 'auth/setup', 'auth/preflight', 'tailscale/status', 'tailscale/serve',
  'check-agent', 'codex/sessions', 'editor/files', 'editor/file', 'git/branch', 'git/status', 'git/sync',
  'layout/commit-diff', 'layout/diff', 'layout/file-content', 'layout/git-log', 'timeline/sessions', 'preflight/runtime',
  `${P}/changes`, `${P}/context-files`, 'uploads/[...path]', 'uploads/cleanup', 'upload-file', 'upload-image',
  `${P}/machine/stop`, 'voice/assets/[...path]', `${P}/file-preview`, `${P}/memory-note`,
];
const routes = files.map((file) => ({
  route: '/api/' + file.replace(/\/index$/, ''),
  load: () => import(`@/pages/api/${file}`),
}));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => {
    if (!handled) {
      res.writeHead(404, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: 'Not found' }));
    }
  });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

// --- requests -----------------------------------------------------------------------------------------------------------
const ses = { cookie: 'session-token=good' };
const agent = { cookie: 'session-token=agent' };
const tok = { 'x-cli-token': TOKEN };
const json = { 'content-type': 'application/json' };
const text = { 'content-type': 'text/plain' };
const form = { 'content-type': 'application/x-www-form-urlencoded' };
const J = (v) => JSON.stringify(v);
const req = (method, p, query, headers, body = '') => ({ method, path: p, query, headers, body });
const file = (p, content) => ({ file: p, content });
const S = { ...ses, ...json };
const A = '/api/platform';
const hashed = 'scrypt:00000000000000000000000000000000:598997e98f4659227a7e76e12e6dd2c8399087265ee249f631c14be51e36b206b3bdc4af99b18331e8c62cbea2cb11bba02f69161c8f47bdbef4a6f39f372cec';
const steps = [
  // the config
  req('GET', '/api/config', [], {}),
  req('GET', '/api/config', [], ses),
  req('PATCH', '/api/config', [], S, J({ appTheme: 'dark' })),
  req('GET', '/api/config', [], ses),
  req('PATCH', '/api/config', [], S, J({ appTheme: 'dark' })),
  req('PATCH', '/api/config', [], S, J({ appTheme: 'light', locale: 'ru', fontSize: 'lg', unknownField: 1, authSecret: 'x' })),
  file('config.json', J({ authSecret: 's'.repeat(64), authPassword: hashed, automationApiToken: 't', updatedAt: '2026-01-01T00:00:00.000Z', locale: 'en', customCSS: '<b>&</b>', hasAuthPassword: 'kept', voiceSettings: { lang: 'en', mode: 'toggle' } })),
  req('GET', '/api/config', [], ses),
  req('GET', '/api/config', [], { ...ses, 'if-none-match': '"x"' }),
  req('PATCH', '/api/config', [], S, J({ acmeLayout: { left: 100, right: 3333.5, collapsed: 'yes' } })),
  req('PATCH', '/api/config', [], S, J({ acmeLayout: { left: 299.5, right: 'x', collapsed: true } })),
  req('PATCH', '/api/config', [], S, J({ acmeLayout: [] })),
  req('PATCH', '/api/config', [], S, J({ acmeLayout: null })),
  req('PATCH', '/api/config', [], S, J({ acmeLayout: 5 })),
  req('PATCH', '/api/config', [], S, J({ voiceSettings: { engine: 'local', key: 'AltRight', deviceId: null } })),
  req('PATCH', '/api/config', [], S, J({ voiceSettings: { engine: 'nope' } })),
  req('PATCH', '/api/config', [], S, J({ voiceSettings: { key: 'KeyA' } })),
  req('PATCH', '/api/config', [], S, J({ voiceSettings: { key: 'F24', autoSend: true, punctuation: false, mode: 'hold' } })),
  req('PATCH', '/api/config', [], S, J({ voiceSettings: [] })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { claude: { model: null, effort: null } } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { codex: {} , claude: [] } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { claude: { model: 'bad name!' } } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { claude: { effort: 'galaxy' } } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { gemini: {} } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { claude: 5 } })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: [] })),
  req('PATCH', '/api/config', [], S, J({ acmeLaunchDefaults: { claude: {} }, toastDuration: 5 })),
  req('PATCH', '/api/config', [], S, J({ editorUrl: 5 })),
  req('PATCH', '/api/config', [], S, J({ editorPreset: 'vim' })),
  req('PATCH', '/api/config', [], S, J({ editorPreset: 'zed', editorUrl: 'zed://' })),
  req('PATCH', '/api/config', [], S, J({ networkAccess: 'lan' })),
  req('PATCH', '/api/config', [], S, J({ networkAccess: 'localhost' })),
  req('PATCH', '/api/config', [], S, J({ gitAskProvider: 'gpt' })),
  req('PATCH', '/api/config', [], S, J({ noteSummaryProvider: 'codex', gitAskProvider: 'claude' })),
  req('PATCH', '/api/config', [], S, J({ toastPositionDesktop: 'middle' })),
  req('PATCH', '/api/config', [], S, J({ toastPositionMobile: 'top-left', toastPositionDesktop: 'bottom-right' })),
  req('PATCH', '/api/config', [], S, J({ toastDuration: 60001 })),
  req('PATCH', '/api/config', [], S, J({ toastDuration: 1000, dangerouslySkipPermissions: 'sure', authPassword: '' })),
  req('PATCH', '/api/config', [], S, J({ authPassword: null, lineHeightCustom: 1.5 })),
  req('PATCH', '/api/config', [], S, J({ authPassword: 'secret-pass' })),
  req('GET', '/api/config', [], ses),
  req('PATCH', '/api/config', [], S, 'null'),
  req('PATCH', '/api/config', [], S, '[1,2]'),
  req('PATCH', '/api/config', [], S, '"text"'),
  req('PATCH', '/api/config', [], S, '{'),
  req('PATCH', '/api/config', [], { ...ses, ...text }, 'appTheme=x'),
  req('PATCH', '/api/config', [], { ...ses, ...form }, 'appTheme=form&locale=a&locale=b'),
  req('POST', '/api/config', [], S, '{}'),
  req('DELETE', '/api/config', [], ses),
  req('HEAD', '/api/config', [], ses),
  req('PATCH', '/agents/api/config', [], { ...tok, ...json }, J({ terminalKeyBar: 'never' })),
  file('config.json', '[1,"x"]'),
  req('GET', '/api/config', [], ses),
  file('config.json', '"ab"'),
  req('GET', '/api/config', [], ses),
  file('config.json', 'null'),
  req('GET', '/api/config', [], ses),
  file('config.json', null),

  // keyboard shortcuts
  req('GET', '/api/keybindings', [], ses),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'tab.new', key: 'ctrl+t' })),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'tab.close', key: null })),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'tab.close' })),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'nope', key: 'x' })),
  req('PATCH', '/api/keybindings', [], S, J({ id: '', key: 'x' })),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'tab.new', key: 5 })),
  req('PATCH', '/api/keybindings', [], S, 'null'),
  req('PATCH', '/api/keybindings', [], { ...ses, ...text }, 'id=tab.new'),
  req('GET', '/api/keybindings', [], ses),
  req('DELETE', '/api/keybindings', [['id', 'tab.new']], ses),
  req('DELETE', '/api/keybindings', [['id', 'unknown']], ses),
  req('DELETE', '/api/keybindings', [['id', 'tab.close'], ['id', 'x']], ses),
  file('keybindings.json', J({ overrides: ['a', null, 3, { x: 1 }] })),
  req('GET', '/api/keybindings', [], ses),
  file('keybindings.json', J({ overrides: { b: 'B', 10: 'ten', 2: null, a: 1, 'tab.new': 'n' } })),
  req('GET', '/api/keybindings', [], ses),
  req('PATCH', '/api/keybindings', [], S, J({ id: 'pane.equalize', key: 'e' })),
  file('keybindings.json', '[]'),
  req('GET', '/api/keybindings', [], ses),
  file('keybindings.json', 'nope'),
  req('GET', '/api/keybindings', [], ses),
  req('DELETE', '/api/keybindings', [['id', '']], ses),
  req('PUT', '/api/keybindings', [], S, '{}'),

  // sent messages
  req('GET', '/api/message-history', [], ses),
  req('GET', '/api/message-history', [['wsId', 'ws-a']], ses),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a', message: 'first' })),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a', message: 'second' })),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a', message: 'first' })),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a', message: '   ' })),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a' })),
  req('POST', '/api/message-history', [], S, J({ message: 'x' })),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-a', message: 5 })),
  req('POST', '/api/message-history', [], S, J({ wsId: 7, message: 'x' })),
  req('POST', '/api/message-history', [], S, 'null'),
  req('POST', '/api/message-history', [], { ...ses, ...text }, 'hello'),
  req('GET', '/api/message-history', [['wsId', 'ws-a']], ses),
  req('GET', '/api/message-history', [['wsId', 'ws-a'], ['wsId', 'ws-b']], ses),
  req('DELETE', '/api/message-history', [], S, J({ wsId: 'ws-a', id: 'id0002' })),
  req('DELETE', '/api/message-history', [], S, J({ wsId: 'ws-a', id: 5 })),
  req('DELETE', '/api/message-history', [], S, J({ wsId: 'ws-a' })),
  req('DELETE', '/api/message-history', [], S, 'null'),
  file('workspaces/ws-b/message-history.json', J({ entries: [{ id: 'k', message: 'm' }, 3, 'str', null] })),
  req('GET', '/api/message-history', [['wsId', 'ws-b']], ses),
  req('POST', '/api/message-history', [], S, J({ wsId: 'ws-b', message: 'm' })),
  req('DELETE', '/api/message-history', [], S, J({ wsId: 'ws-b', id: 'k' })),
  file('workspaces/ws-c/message-history.json', J({ entries: { a: 1 } })),
  req('GET', '/api/message-history', [['wsId', 'ws-c']], ses),
  req('PATCH', '/api/message-history', [], S, '{}'),

  // quick prompts and sidebar items
  ...['quick-prompts', 'sidebar-items'].flatMap((name) => [
    req('GET', `/api/${name}`, [], ses),
    req('PUT', `/api/${name}`, [], S, J({ custom: [{ id: 'c1', name: 'C', extra: true }], disabledBuiltinIds: ['builtin-commit', 'builtin-stats'], order: ['c1'], more: 1 })),
    req('GET', `/api/${name}`, [], ses),
    req('PUT', `/api/${name}`, [], S, J({ custom: [], disabledBuiltinIds: [] })),
    req('PUT', `/api/${name}`, [], S, 'null'),
    req('PUT', `/api/${name}`, [], { ...ses, ...text }, 'x'),
    file(`${name}.json`, J({ custom: ['ab', 3, null, [1]], disabledBuiltinIds: 'builtin-notes', order: 'o' })),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, J({ custom: 'x' })),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, J({ disabledBuiltinIds: {} })),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, J([{ id: 'builtin-simplify', enabled: false }, { id: 'mine', name: 'M', enabled: true }, 4])),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, J([null])),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, 'null'),
    req('GET', `/api/${name}`, [], ses),
    file(`${name}.json`, '5'),
    req('GET', `/api/${name}`, [], ses),
    req('POST', `/api/${name}`, [], S, '{}'),
  ]),

  // automations: a machine reaches no database
  req('GET', `${A}/automations/rules`, [], ses),
  req('GET', `${A}/automations/rules`, [], {}),
  file('config.json', J({ automationOwnerEmail: '' })),
  req('GET', `${A}/automations/rules`, [], ses),
  file('config.json', J({ automationOwnerEmail: 'owner@example.org' })),
  req('GET', `${A}/automations/rules`, [], ses),
  req('POST', `${A}/automations/rules`, [], S, '{}'),
  req('GET', `${A}/automations/rules/r1`, [], ses),
  req('GET', `${A}/automations/runs`, [], ses),
  req('DELETE', `${A}/automations/runs`, [], ses),
  req('GET', `${A}/automations/rules`, [], { ...tok }),
  req('GET', `${A}/automations/rules`, [], { ...tok, authorization: 'Bearer wrong' }),
  req('GET', `${A}/automations/token`, [], tok),
  req('POST', `${A}/automations/token`, [], ses),
  req('GET', `${A}/automations/token`, [], ses),
  req('GET', `${A}/automations/token`, [], ses),
  req('GET', `${A}/automations/runs`, [], { ...tok, authorization: `Bearer ${'0'.repeat(56)}00000005` }),
  req('PATCH', `${A}/automations/runs`, [], { ...tok, authorization: `Bearer ${'0'.repeat(56)}00000005` }),
  req('GET', `${A}/automations/runs`, [], { ...tok, authorization: `Bearer ${'é'.repeat(64)}` }),
  file('config.json', null),

  // the page's socket report, the gateway's probe
  req('POST', `${A}/net-events`, [], S, J({ sockets: { status: { drops: 2, dead: 1.5, recovered: -1, downMs: [1, 2.5, 30000] }, 'bad key!': {}, x: 5 }, hidden: true })),
  req('POST', `${A}/net-events`, [], { ...ses, ...text }, J({ sockets: [{ drops: 1 }] })),
  req('POST', `${A}/net-events`, [], { ...ses, ...text }, 'not json'),
  req('POST', `${A}/net-events`, [], S, J({ sockets: null })),
  req('POST', `${A}/net-events`, [], { ...tok, ...json }, J({ sockets: {} })),
  req('GET', `${A}/net-events`, [], ses),
  req('GET', `${A}/gateway-ping`, [], ses),
  req('POST', `${A}/gateway-ping`, [], ses),
  req('GET', `${A}/gateway-ping`, [], {}),

  // dead routes
  req('GET', '/api/stats/daily-report/cache', [], ses),
  req('POST', '/api/stats/daily-report/cache', [], ses),
  req('GET', '/api/react-scan', [], ses),
  file('../node_modules/react-scan/dist/auto.global.js', 'window.scan=1;'),
  req('GET', '/api/react-scan', [], ses),
  req('GET', '/api/react-scan', [], { ...ses, 'if-none-match': '"7QhBxo-q1IbcQ36nyqHXjbGKLaQ"' }),
  req('POST', '/api/auth/login', [], json, J({ password: 'x' })),
  req('GET', '/api/auth/login', [], {}),
  req('POST', '/api/auth/login', [], json, 'null'),
  req('POST', '/api/auth/logout', [], {}),
  req('GET', '/api/auth/setup', [], {}),
  req('GET', '/api/auth/preflight', [], { 'content-type': 'application/json' }, '{'),
  req('POST', '/api/auth/preflight', [], {}),
  req('POST', '/api/auth/setup', [], json, J({ authPassword: '' })),
  req('POST', '/api/auth/setup', [], json, J({ authPassword: 'pw-1', terminalTheme: { light: 'a', dark: 'b' }, networkAccess: 'tailscale', locale: '', dangerouslySkipPermissions: null })),
  req('POST', '/api/auth/setup', [], json, J({ authPassword: 'pw-2' })),
  req('GET', '/api/auth/setup', [], {}),
  req('PUT', '/api/auth/setup', [], {}),
  req('GET', '/api/auth/preflight', [], {}),
  req('POST', '/api/auth/login', [], json, J({ password: 'wrong' })),
  req('POST', '/api/auth/login', [], json, J({ password: 5 })),
  req('POST', '/api/auth/login', [], { ...json, 'x-forwarded-proto': 'https' }, J({ password: 'pw-1' })),
  file('config.json', null),
  ...Array.from({ length: 16 }, () => req('POST', '/api/auth/login', [], json, J({ password: 'x' }))),
  req('POST', '/api/auth/login', [], json, J({ password: 'x' })),
  req('GET', '/api/tailscale/status', [], ses),
  req('POST', '/api/tailscale/serve', [], S, J({ httpsPort: 443, localPort: 8022 })),
  req('POST', '/api/tailscale/serve', [], S, J({ httpsPort: '443', localPort: 0 })),
  { path_: 'bin' },
  req('GET', '/api/tailscale/status', [], ses),
  req('POST', '/api/tailscale/serve', [], S, J({ httpsPort: ' 8443 ', localPort: true })),
  req('DELETE', '/api/tailscale/serve', [], S, J({ httpsPort: 443 })),
  req('DELETE', '/api/tailscale/serve', [], S, J({ httpsPort: 1.5 })),
  req('GET', '/api/tailscale/serve', [], ses),
  req('PUT', '/api/tailscale/status', [], ses),

  // other methods of the routes the other slices answer
  ...[
    ['/api/check-agent', ['POST', 'HEAD', 'DELETE']], ['/api/codex/sessions', ['POST']],
    ['/api/editor/files', ['POST']], ['/api/editor/file', ['POST', 'DELETE', 'HEAD']], ['/api/git/branch', ['POST']],
    ['/api/git/status', ['DELETE']], ['/api/layout/commit-diff', ['PUT']], ['/api/layout/diff', ['POST']],
    ['/api/layout/file-content', ['PATCH']], ['/api/layout/git-log', ['POST']], ['/api/timeline/sessions', ['DELETE']],
    ['/api/preflight/runtime', ['PUT', 'HEAD']], [`${A}/changes`, ['PUT', 'HEAD']], [`${A}/context-files`, ['POST']],
    ['/api/uploads/ws/tab/x.png', ['POST', 'DELETE']], ['/api/git/sync', ['GET', 'HEAD']], ['/api/uploads/cleanup', ['GET']],
    ['/api/upload-file', ['GET', 'PUT']], ['/api/upload-image', ['DELETE']], [`${A}/machine/stop`, ['GET', 'PUT']],
    ['/api/voice/assets/models/x.bin', ['POST']],
  ].flatMap(([p, methods]) => methods.map((m) => req(m, p, [], S, m === 'GET' || m === 'HEAD' ? '' : '{}'))),
  req('POST', '/api/git/branch', [], S, '{'),
  req('PUT', `${A}/changes`, [], { ...tok, ...json }, '{}'),
  req('POST', `${A}/context-files`, [], { ...tok, ...json }, '{}'),
  req('PUT', '/api/editor/file', [], S, 'x'.repeat(10)),

  // a file preview checked by POST, any other method serving one; a memory note by any method
  req('POST', `${A}/file-preview`, [], S, J({ paths: [`${work}/a.png`, `${work}/notes.md`, `${work}/data.bin`, `${work}/missing.png`, 5, 'rel.png'] })),
  req('POST', `${A}/file-preview`, [], S, J({ paths: [`${work}/a.png`, `${work}/data.bin`, `${work}/missing.png`, '/etc/passwd', 'rel'], details: true })),
  req('POST', `${A}/file-preview`, [], S, J({ paths: 'x' })),
  req('POST', `${A}/file-preview`, [], { ...ses, ...text }, 'x'),
  req('POST', `${A}/file-preview`, [], { ...tok, ...json }, '{}'),
  req('PUT', `${A}/file-preview`, [['path', `${work}/notes.md`]], S, '{}'),
  req('DELETE', `${A}/file-preview`, [['path', `${work}/data.bin`], ['download', '1']], S, '{}'),
  req('POST', `${A}/file-preview`, [], S, '{'),
  req('POST', `${A}/memory-note`, [['name', 'fact.md']], S, '{}'),
  req('DELETE', `${A}/memory-note`, [['name', 'none.md']], S, '{}'),
  req('PATCH', `${A}/memory-note`, [['name', 'fact.md']], { ...tok, ...json }, '{}'),
  req('POST', `${A}/memory-note`, [['name', 'fact.md']], S, '{'),

  // repeated and malformed queries of the git routes
  req('GET', '/api/git/branch', [['tmuxSession', 'tab-1'], ['tmuxSession', 'tab-1']], ses),
  req('GET', '/api/git/branch', [['tmuxSession', '']], ses),
  req('GET', '/api/git/status', [['tmuxSession', 'a'], ['tmuxSession', 'b']], ses),
  req('GET', '/api/layout/diff', [['session', 'tab-1'], ['session', 'tab-1']], ses),
  req('GET', '/api/layout/diff', [['session', 'tab-plain'], ['hashOnly', 'true'], ['hashOnly', 'true']], ses),
  req('GET', '/api/layout/commit-diff', [['session', 'tab-1'], ['hash', 'abcd'], ['hash', 'abcd']], ses),
  req('GET', '/api/layout/commit-diff', [['session', 'a'], ['session', 'b'], ['hash', 'abcd']], ses),
  req('GET', '/api/layout/commit-diff', [['session', 'a'], ['session', 'b'], ['hash', 'zz']], ses),
  req('GET', '/api/layout/commit-diff', [['session', 'a'], ['session', 'b']], ses),
  req('GET', '/api/layout/git-log', [['session', 'x'], ['session', 'y'], ['limit', '5'], ['limit', '6']], ses),
  req('GET', '/api/layout/git-log', [['session', 'tab-plain'], ['limit', '5'], ['limit', '6']], ses),
  { method: 'GET', path: '/api/git/branch', raw: 'tmuxSession=%zz&force=%', headers: ses, body: '' },
  { method: 'GET', path: '/api/layout/diff', raw: 'session=tab-plain&%zz', headers: ses, body: '' },
  { method: 'GET', path: `${A}/changes`, raw: 'root=%E0%A4%A&sha=x&sha=y', headers: ses, body: '' },
  // the diff that fetches first
  req('GET', '/api/layout/diff', [['session', 'tab-1'], ['hashOnly', 'true']], ses),
  req('GET', '/api/layout/diff', [['session', 'tab-1'], ['hashOnly', 'true'], ['fetch', 'true']], ses),
  req('GET', '/api/layout/diff', [['session', 'tab-broken'], ['hashOnly', 'true'], ['fetch', 'true']], ses),
  req('GET', '/api/layout/diff', [['session', 'tab-1'], ['fetch', 'true'], ['fetch', 'true'], ['hashOnly', 'true']], ses),
  req('POST', '/api/git/sync', [['session', 'a'], ['session', 'b']], { ...tok }, ''),
  req('POST', '/api/git/sync', [['session', ''], ['session', 'b']], { ...tok }, ''),
];

// OLD_RANDOM_CASES=<n> replaces the list with n generated requests (seed OLD_RANDOM_SEED), written to OLD_OUT: a one-off
// run against the Go test (STATE_GOLDEN_FILE=<that file>), not kept.
if (process.env.OLD_RANDOM_CASES) {
  let seed = Number(process.env.OLD_RANDOM_SEED ?? 1) >>> 0;
  const rnd = () => { seed = (seed + 0x6d2b79f5) >>> 0; let t = seed; t = Math.imul(t ^ (t >>> 15), t | 1); t ^= t + Math.imul(t ^ (t >>> 7), t | 61); return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  const values = [null, true, false, 0, -1, 1.5, 999, 1000, 60000, 60001, 3200.4, 199.5, '', ' ', 'x', 'dark', 'ru', 'zed', 'vim', 'claude', 'codex',
    'gpt', 'localhost', 'tailscale', 'all', 'lan', 'top-left', 'middle', 'never', 'AltLeft', 'F13', 'F0', 'KeyA', 'hold', 'toggle', 'auto',
    'local', 'sonnet', 'opus', 'bad name', 'high', 'max', 'galaxy', [], [1, 'a'], {}, { a: 1 }, { left: 250, right: 500, collapsed: true }];
  const fields = ['appTheme', 'terminalTheme', 'customCSS', 'dangerouslySkipPermissions', 'claudeShowTerminal', 'gitAskProvider',
    'noteSummaryProvider', 'editorUrl', 'editorPreset', 'notificationsEnabled', 'toastOnCompleteEnabled', 'toastDuration',
    'toastPositionDesktop', 'toastPositionMobile', 'locale', 'fontSize', 'lineHeight', 'lineHeightCustom', 'terminalKeyBar',
    'systemResourcesEnabled', 'networkAccess', 'acmeLayout', 'voiceSettings', 'acmeLaunchDefaults', 'authSecret', 'other', '0'];
  const voice = () => Object.fromEntries(Array.from({ length: Math.floor(rnd() * 3) }, () => [pick(['engine', 'lang', 'mode', 'autoSend', 'punctuation', 'deviceId', 'key', 'z']), pick(values)]));
  const value = (k) => {
    if (k === 'voiceSettings' && rnd() < 0.7) return voice();
    if (k === 'acmeLayout' && rnd() < 0.7) return { left: pick(values), right: pick(values), collapsed: pick(values) };
    if (k === 'acmeLaunchDefaults' && rnd() < 0.7) return Object.fromEntries(Array.from({ length: Math.floor(rnd() * 3) }, () => [pick(['claude', 'codex', 'x']), rnd() < 0.8 ? { model: pick(values), effort: pick(values) } : pick(values)]));
    return pick(values);
  };
  const actions = ['tab.new', 'tab.close', 'view.notes', 'app.settings', 'nope', '', '0'];
  const ws = ['ws-a', 'ws-b', 'ws-c', '', 'w x'];
  const bodies = (gen) => (rnd() < 0.08 ? pick(['null', '[1]', '"s"', '{', '5']) : J(gen()));
  const gens = [
    () => req('PATCH', '/api/config', [], S, bodies(() => Object.fromEntries(Array.from({ length: 1 + Math.floor(rnd() * 4) }, () => { const k = pick(fields); return [k, value(k)]; })))),
    () => req('GET', '/api/config', [], ses),
    () => file('config.json', rnd() < 0.15 ? pick([null, '[]', 'null', '{"voiceSettings":"ab"}', '{"acmeLaunchDefaults":[1]}']) : J({ updatedAt: '2026-01-01T00:00:00.000Z', voiceSettings: voice(), acmeLaunchDefaults: { claude: { model: null, effort: null } }, authPassword: pick([hashed, 'plain', 5]) })),
    () => req('PATCH', '/api/keybindings', [], S, bodies(() => ({ id: pick(actions), key: pick([null, 'k', 5, undefined]) }))),
    () => req('DELETE', '/api/keybindings', rnd() < 0.5 ? [['id', pick(actions)]] : [['id', pick(actions)], ['id', pick(actions)]], ses),
    () => req('GET', '/api/keybindings', [], ses),
    () => file('keybindings.json', rnd() < 0.2 ? pick(['[]', 'x', '{"overrides":5}']) : J({ overrides: Object.fromEntries(Array.from({ length: 3 }, () => [pick(actions.concat(['7', '12'])), pick([null, 'a', 3, {}])])) })),
    () => req('POST', '/api/message-history', [], S, bodies(() => ({ wsId: pick(ws.concat([5, null])), message: pick(['hi', 'there', ' ', '', 5, null, 'hi']) }))),
    () => req('DELETE', '/api/message-history', [], S, bodies(() => ({ wsId: pick(ws), id: pick(['id0001', 'id0002', 'id0003', 5, '', 'k']) }))),
    () => req('GET', '/api/message-history', rnd() < 0.9 ? [['wsId', pick(ws)]] : [['wsId', 'a'], ['wsId', 'b']], ses),
    () => req('PUT', `/api/${pick(['quick-prompts', 'sidebar-items'])}`, [], S, bodies(() => ({ custom: pick([[], [{ id: 'c' }], 'x', ['s', 2]]), disabledBuiltinIds: pick([[], ['builtin-commit'], ['builtin-notes'], 'b', {}]), order: pick([[], ['c'], 'o', null]) }))),
    () => req('GET', `/api/${pick(['quick-prompts', 'sidebar-items'])}`, [], ses),
    () => file(`${pick(['quick-prompts', 'sidebar-items'])}.json`, J(pick([{ custom: [{ id: 'q' }], disabledBuiltinIds: ['builtin-simplify'] }, [{ id: 'builtin-commit', enabled: false }, { id: 'x' }], null, 3, { custom: {} }]))),
    () => req('POST', `${A}/net-events`, [], pick([S, { ...ses, ...text }]), J({ sockets: pick([{ a: { drops: pick(values), downMs: [pick(values), pick(values)] } }, [{ dead: 2 }], null, 'x']), online: pick(values), hidden: pick(values) })),
    () => req(pick(['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD']), pick(['/api/check-agent', '/api/git/branch', '/api/editor/file', `${A}/changes`, `${A}/context-files`, '/api/upload-file', `${A}/machine/stop`, '/api/preflight/runtime']), [], pick([S, { ...tok, ...json }, {}]), pick(['', '{}', '{'])),
    () => req(pick(['POST', 'DELETE']), '/api/tailscale/serve', [], S, J({ httpsPort: pick(values), localPort: pick(values) })),
  ];
  steps.length = 0;
  steps.push(file('config.json', null));
  for (let n = Number(process.env.OLD_RANDOM_CASES); n > 0; n--) steps.push(pick(gens)());
}

// --- run ------------------------------------------------------------------------------------------------------------------
const snapshot = () => {
  const out = [];
  const walk = (d) => {
    let names;
    try { names = fs.readdirSync(d).sort(); } catch { return; }
    for (const n of names) {
      const p = path.join(d, n);
      const st = fs.lstatSync(p);
      const rel = path.relative(data, p);
      if (rel === 'cli-token') continue;
      if (st.isDirectory()) { out.push({ path: rel, dir: true }); walk(p); }
      else out.push({ path: rel, mode: (st.mode & 0o777).toString(8), text: neutral(fs.readFileSync(p, 'utf8')) });
    }
  };
  walk(data);
  return out;
};
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => {
  if (NEUTRAL_TOKENS.includes(k)) return [OLD_TOKENS[NEUTRAL_TOKENS.indexOf(k)], v];
  return [k, v];
}));
const swap = (s, from, to) => WORDS.reduce((acc, w) => acc.split(w[from]).join(w[to]), s);
const old = (s) => swap(s, 0, 1);
const neutral = (s) => swap(s, 1, 0).split(root).join('$ROOT');
const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length', 'set-cookie', 'content-disposition', 'x-content-type-options'];
const out = [];
let prev = J(snapshot());
let i = 0;
for (const step of steps) {
  i += 1;
  clock = FIXED + i * 1000;
  if (step.file !== undefined) {
    const p = path.join(data, step.file);
    if (step.content === null) fs.rmSync(p, { force: true });
    else { fs.mkdirSync(path.dirname(p), { recursive: true }); fs.writeFileSync(p, old(step.content)); }
    out.push({ file: step.file, content: step.content });
    prev = J(snapshot());
    continue;
  }
  if (step.path_ !== undefined) {
    process.env.PATH = `${path.join(root, step.path_)}:/usr/bin:/bin`;
    out.push({ pathDir: step.path_ });
    continue;
  }
  const { method, path: urlPath, query = [], raw, headers, body } = step;
  const qs = raw ?? query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
  const url = urlPath.replace('/api/platform', OLD_API) + (qs ? `?${qs}` : '');
  const payload = Buffer.from(old(body), 'utf8');
  const res = await new Promise((resolve) => {
    const r = http.request(base + url, { method, headers: { ...toOld(headers), 'content-length': payload.length } }, (resp) => {
      const chunks = [];
      resp.on('data', (c) => chunks.push(c));
      resp.on('end', () => resolve({ status: resp.statusCode, headers: resp.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    r.on('error', (err) => resolve({ status: 0, headers: {}, text: String(err.code) }));
    r.end(payload);
  });
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = neutral(String(res.headers[k]));
  const scrubbed = (v) => (v === undefined ? v : v.split(root).join('$ROOT'));
  const entry = { method, path: urlPath, query: query.map(([k, v]) => [k, scrubbed(v)]), raw: scrubbed(raw), headers, body: scrubbed(body), at: clock, status: res.status, answerHeaders: kept, answer: neutral(res.text) };
  const now = J(snapshot());
  if (now !== prev) { entry.files = JSON.parse(now); prev = now; }
  out.push(entry);
}
server.close();
const target = process.env.OLD_OUT || path.join(here, '..', 'state-golden.json');
fs.writeFileSync(target, JSON.stringify({ now: FIXED, token: TOKEN, gitSetup: GIT_SETUP, fixtures, steps: out }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} steps → ${target}`);
