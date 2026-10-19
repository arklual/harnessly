// Sends a fixed list of requests to the previous runtime's own API dispatcher and launch handlers (conversations,
// automations, documents, evals, side chats, Codex lines, the CLI's agent tabs, resumes), on fixture files with tmux and
// the status keeper recorded, then runs its startup resume. What each request answered, what it typed and registered,
// and the files left go to ../launch-routes-golden.json; the Go test replays the requests on its routes and must match.
//
// The previous runtime's product names are not spelled here: the caller passes them, and the output is neutralised
// (see NAMES below) before it is written.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> OLD_PLATFORM_API=<its platform route prefix> \
//   OLD_META_KEY=<its tab metadata key> OLD_ENV_PREFIX=<its launch variable prefix> OLD_WORKSPACE_VAR=<its workspace root \
//   variable> OLD_CONFIG_DIR=<its folder under ~/.config> OLD_MODULE_PREFIX=<its modules' name prefix> OLD_NAMES='<json map of every product name → neutral name>' \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const NEEDED = ['OLD_NAMES', 'OLD_MODULE_PREFIX', 'OLD_RUNTIME_TOKEN_HEADER', 'OLD_PLATFORM_API', 'OLD_META_KEY', 'OLD_ENV_PREFIX', 'OLD_WORKSPACE_VAR', 'OLD_CONFIG_DIR'];
for (const k of NEEDED) if (!process.env[k]) throw new Error(`${k} is required`);
const NAMES = Object.entries(JSON.parse(process.env.OLD_NAMES)).sort((a, b) => b[0].length - a[0].length);
const { OLD_PLATFORM_API: P, OLD_META_KEY: META, OLD_ENV_PREFIX: ENV } = process.env;

const FIXED = Date.parse('2026-10-18T09:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'launch-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
process.env.PORT = '18123';
const projects = path.join(root, 'projects');
const A = path.join(projects, 'proj-a');
const B = path.join(projects, 'proj-b');
const C = path.join(projects, 'proj-c');
for (const d of [A, B, C, path.join(root, 'outside')]) fs.mkdirSync(d, { recursive: true });
process.env[process.env.OLD_WORKSPACE_VAR] = projects;
process.chdir(root);
// The bundled runtime is CommonJS; the eval route looks next to its own file too.
globalThis.__dirname = path.join(root, 'nowhere');
fs.mkdirSync(path.join(root, 'dist'));
fs.writeFileSync(path.join(root, 'dist', 'eval-runner.js'), '');

const SID = '425055ec-bbc8-4e3c-87d0-36fa74c1cccb';
const SID2 = '019a0c11-2222-7333-8444-555555555555';
const SID3 = '7a1c2d3e-4f50-4617-8899-aabbccddeeff';
const RUN = '11111111-2222-4333-8444-555555555555';
const RUN2 = '22222222-2222-4333-8444-555555555555';
const RUN3 = '33333333-2222-4333-8444-555555555555';
const TOKEN = 'a'.repeat(64);

globalThis.__golden = {
  tmux: {
    sessions: new Set(['tk-ws-a-pane-a-tab-1', 'tk-ws-a-pane-a-tab-2', 'tk-ws-a-pane-a-tab-3', 'tk-ws-a-pane-a-tab-5', 'tk-ws-a-pane-a-tab-6']),
    cwd: { 'tk-ws-a-pane-a-tab-2': A, 'tk-ws-a-pane-a-tab-3': B },
    panes: { '%3': [['main', 'tk-ws-a-pane-a-tab-2']], '%4': [['main', 'tk-ws-a-pane-a-tab-1'], ['old', 'tk-ws-a-pane-a-tab-3']] },
    commands: { 'tk-ws-a-pane-a-tab-5': 'claude', 'tk-ws-a-pane-a-tab-6': 'vim' },
    created: { 'tk-ws-a-pane-a-tab-1': 1790000000 },
  },
  sources: [
    { tabId: 'tab-2', tmuxSession: 'tk-ws-a-pane-a-tab-2', cliState: 'idle', agentSessionId: SID, provider: 'claude', panelType: 'claude-code' },
    { tabId: 'tab-3', tmuxSession: 'tk-ws-a-pane-a-tab-3', cliState: 'busy', agentSessionId: SID2, provider: 'codex', panelType: 'codex-cli' },
  ],
  missing: [],
  calls: [],
};

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const { autoResumeForCore } = await import('@/lib/auto-resume');
const DATA = dataDir();
// The workspace prompt files hold the platform's own text and are the previous runtime's to write: not here.
const { getProvider } = await import('@/lib/providers');
for (const id of ['claude', 'codex']) getProvider(id).writeWorkspacePrompt = async () => {};
const claudeProject = path.join(home, '.claude', 'projects', A.replace(/[^a-zA-Z0-9]/g, '-'));
const codexDay = path.join(home, '.codex', 'sessions', '2026', '10', '18');
const transcript = path.join(claudeProject, `${SID}.jsonl`);
const codexTranscript = path.join(codexDay, `rollout-2026-10-18T08-00-00-${SID2}.jsonl`);
const fakeClaude = path.join(root, 'bin', 'fake-claude');

const files = {
  'cli-token': TOKEN,
  'config.json': JSON.stringify({ dangerouslySkipPermissions: false }),
  'workspaces.json': JSON.stringify({
    workspaces: [{ id: 'ws-a', name: 'Alpha', directories: [A] }, { id: 'ws-b', name: 'Beta', directories: [B] }],
    groups: [], sidebarCollapsed: false, sidebarWidth: 240, updatedAt: '2026-01-01T00:00:00.000Z',
  }, null, 2),
  'workspaces/ws-a/codex-prompt.md': 'Work "carefully".\nSecond line',
  'workspaces/ws-a/layout.json': JSON.stringify({
    root: {
      type: 'pane', id: 'pane-a', activeTabId: 'tab-1',
      tabs: [
        { id: 'tab-1', sessionName: 'tk-ws-a-pane-a-tab-1', name: '', order: 0, cwd: A },
        { id: 'tab-2', sessionName: 'tk-ws-a-pane-a-tab-2', name: 'Claude', order: 1, panelType: 'claude-code', cliState: 'idle',
          agentState: { providerId: 'claude', sessionId: SID, jsonlPath: transcript, summary: 'Old summary' }, claudeSessionId: SID,
          [META]: { launch: { model: 'opus', effort: null } } },
        { id: 'tab-3', sessionName: 'tk-ws-a-pane-a-tab-3', name: 'Codex', order: 2, panelType: 'codex-cli', cliState: 'busy',
          agentState: { providerId: 'codex', sessionId: SID2, jsonlPath: codexTranscript, summary: null }, [META]: { mode: 'automation', launch: { model: 'gpt-5', effort: 'high' } } },
        { id: 'tab-4', sessionName: 'tk-ws-a-pane-a-tab-4', name: 'Gone', order: 3, panelType: 'claude-code', cliState: 'busy',
          agentState: { providerId: 'claude', sessionId: SID3, jsonlPath: null, summary: null }, lastUserMessage: 'last words' },
        { id: 'tab-5', sessionName: 'tk-ws-a-pane-a-tab-5', name: 'Running', order: 4, panelType: 'claude-code', cliState: 'needs-input',
          agentState: { providerId: 'claude', sessionId: SID, jsonlPath: null, summary: null } },
        { id: 'tab-6', sessionName: 'tk-ws-a-pane-a-tab-6', name: 'Editor', order: 5, panelType: 'claude-code', cliState: 'busy',
          claudeSessionId: SID3, title: 'vim title' },
      ],
    },
    activePaneId: 'pane-a', updatedAt: '2026-01-01T00:00:00.000Z',
  }, null, 2),
  [`eval/runs/${RUN2}/state.json`]: JSON.stringify({ state: 'running' }),
  [`eval/runs/${RUN3}/state.json`]: JSON.stringify({ state: 'skipped' }),
};
for (const [name, content] of Object.entries(files)) {
  fs.mkdirSync(path.dirname(path.join(DATA, name)), { recursive: true });
  fs.writeFileSync(path.join(DATA, name), content, { mode: 0o600 });
}
const outside = {
  [transcript]: [
    JSON.stringify({ type: 'user', message: { role: 'user', content: 'first prompt' }, timestamp: '2026-10-17T10:00:00.000Z' }),
    JSON.stringify({ type: 'ai-title', aiTitle: 'Fix the \"login\" bug' }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }] }, timestamp: '2026-10-17T10:00:05.000Z' }),
    JSON.stringify({ type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'second prompt' }] }, timestamp: '2026-10-17T11:00:00.000Z' }),
    JSON.stringify({ type: 'user', message: { role: 'user', content: '<command-name>/clear</command-name>' }, timestamp: '2026-10-17T12:00:00.000Z' }),
    '',
  ].join('\n'),
  [codexTranscript]: [
    JSON.stringify({ timestamp: '2026-10-18T08:00:00.000Z', type: 'session_meta', payload: { id: SID2, cwd: B, timestamp: '2026-10-18T08:00:00.000Z' } }),
    JSON.stringify({ timestamp: '2026-10-18T08:01:00.000Z', type: 'event_msg', payload: { type: 'user_message', message: 'codex prompt' } }),
    '',
  ].join('\n'),
  [fakeClaude]: [
    '#!/bin/sh',
    'cat >/dev/null',
    'echo \'{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}\'',
    'echo \'not json\'',
    'echo \'{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}}\'',
    'printf \'{"type":"result","result":"Hello","is_error":false,"total_cost_usd":0.25}\'',
    '',
  ].join('\n'),
  [path.join(home, '.config', process.env.OLD_CONFIG_DIR, 'runtime.json')]: JSON.stringify({ claude_binary: fakeClaude }),
};
for (const [file, content] of Object.entries(outside)) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content, { mode: 0o700 });
}

const routes = [
  `${P}/conversations`, `${P}/automations/start`, `${P}/docs/deliver`, `${P}/docs/resolve-pane`,
  `${P}/eval/start`, `${P}/side-chat`, '/api/codex/launch-args', '/api/codex/launch-command', '/api/cli/tabs',
  '/api/workspace', '/api/layout/pane/[paneId]/tabs',
].map((route) => ({ route, load: () => import(`@/pages${route}`) }));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const OLD_HEADER = process.env.OLD_RUNTIME_TOKEN_HEADER;
const tok = { [OLD_HEADER]: TOKEN };
const ses = { cookie: 'session-token=good' };
const agent = { cookie: 'session-token=agent' };
const json = { 'content-type': 'application/json' };
const long = `Long task ${'x'.repeat(2400)}`;
const requests = [
  ['GET', `${P}/conversations`, tok],
  ['GET', `${P}/conversations`, ses],
  ['GET', `/agents${P}/conversations`, { ...ses, 'if-none-match': 'ETAG' }],
  ['PUT', `${P}/conversations`, ses],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"grok"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","workspaceId":5}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","model":"Bad Model"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","effort":"turbo"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","model":"no-such-model"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","workspaceId":"ws-x"}'],
  { missing: ['claude-code'] },
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","prompt":"hi"}'],
  { missing: [] },
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"claude","workspaceId":"ws-a","prompt":"  Fix the build\\nnow \'quoted\'  ","model":"opus","effort":"high"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, '{"provider":"codex","workspaceId":"ws-b"}'],
  ['POST', `${P}/conversations`, { ...ses, ...json }, JSON.stringify({ provider: 'claude', prompt: long, model: null })],
  ['POST', `${P}/conversations`, ses, 'provider=claude'],
  ['POST', `${P}/automations/start`, { ...ses, ...json }, '{"prompt":"x"}'],
  ['GET', `${P}/automations/start`, agent],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, '{"prompt":"   "}'],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, '{"prompt":"run","model":"Bad Model"}'],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, '{"prompt":"run","env":{"PATH":"/bin"}}'],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, `{"prompt":"run","env":{"${ENV}RUN":5}}`],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, '{"prompt":"run","env":["x"]}'],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, `{"prompt":"run","cwd":"${path.join(root, 'outside')}"}`],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, '{"prompt":"run","cwd":"relative/dir"}'],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, `{"prompt":"run","cwd":"${path.join(projects, 'missing')}"}`],
  ['POST', `${P}/automations/start`, { ...agent, ...json }, `{"prompt":"Nightly check","workspaceId":"ws-a","cwd":"${C}","env":{"${ENV}RUN_ID":"r-1","${ENV}RESULT_TOKEN":"abc.DEF_9"}}`],
  ['POST', `${P}/docs/resolve-pane`, { ...agent, ...json }, '{}'],
  ['POST', `${P}/docs/resolve-pane`, { ...agent, ...json }, '{"tmuxPane":"%3"}'],
  ['POST', `${P}/docs/resolve-pane`, { ...agent, ...json }, '{"tmuxPane":"%4"}'],
  ['POST', `${P}/docs/resolve-pane`, { ...agent, ...json }, '{"tmuxPane":"%4","tmuxSocket":"old"}'],
  ['POST', `${P}/docs/resolve-pane`, { ...agent, ...json }, '{"tmuxPane":"%9"}'],
  ['POST', `${P}/docs/deliver`, { ...agent, ...json }, '{"message":""}'],
  ['POST', `${P}/docs/deliver`, { ...agent, ...json }, '{"message":"Please fix the heading","tabId":"tab-2"}'],
  ['POST', `${P}/docs/deliver`, { ...agent, ...json }, `{"message":"And the footer","agentSessionId":"${SID2}"}`],
  ['POST', `${P}/docs/deliver`, { ...agent, ...json }, `{"message":"Reopen","agentSessionId":"${SID3}","cwd":"${A}"}`],
  ['POST', `${P}/docs/deliver`, { ...agent, ...json }, `{"message":"From outside","cwd":"${path.join(root, 'outside')}"}`],
  ['POST', `${P}/eval/start`, { ...ses, ...json }, '{}'],
  ['POST', `${P}/eval/start?stop=nope`, { ...agent, ...json }, '{}'],
  ['POST', `${P}/eval/start?stop=${RUN}`, { ...agent, ...json }, '{}'],
  ['POST', `${P}/eval/start`, { ...agent, ...json }, '{"runId":"x"}'],
  ['POST', `${P}/eval/start`, { ...agent, ...json }, JSON.stringify({ runId: RUN2, task: { prompt: 'p', base: 'main', checks: [] }, arm: { model: 'opus', harness: 'repo' } })],
  ['POST', `${P}/eval/start`, { ...agent, ...json }, JSON.stringify({ runId: RUN3, task: { prompt: 'p', base: 'main', checks: [{ run: 'true' }] }, arm: { model: 'opus', harness: 'personal' } })],
  ['GET', `${P}/side-chat`, tok],
  ['GET', `${P}/side-chat`, ses],
  ['POST', `${P}/side-chat`, ses, '{"tabId":"tab-2"}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"question":"why?"}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"tabId":"tab-9","question":"why?"}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"tabId":"tab-3","question":"why?"}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"tabId":"tab-2","question":"why?","excerpts":[{"quote":"   "}]}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"tabId":"tab-2","question":"why?","model":"Bad"}'],
  ['POST', `${P}/side-chat`, { ...ses, ...json }, '{"tabId":"tab-2","question":"why <question>?","excerpts":[{"quote":"a","source":{"entryId":"e1","role":"agent"}}],"history":[{"question":"q","answer":"a"}],"model":"haiku"}'],
  ['GET', '/api/codex/launch-args', tok],
  ['POST', '/api/codex/launch-args', { ...tok, ...json }, '{}'],
  ['POST', '/api/codex/launch-args', { ...tok, ...json }, `{"workspaceId":" ws-a ","resumeSessionId":"${SID2}","model":"gpt-5","effort":"high"}`],
  ['POST', '/api/codex/launch-args', { ...tok, ...json }, '{"model":"Bad Model"}'],
  ['POST', '/api/codex/launch-args', { ...tok, ...json }, '{"resumeSessionId":"not-a-session"}'],
  ['POST', '/api/codex/launch-command', { ...tok, ...json }, '{}'],
  ['POST', '/api/codex/launch-command', { ...ses, ...json }, `{"workspaceId":"ws-b","resumeSessionId":"${SID2}","effort":"low"}`],
  ['POST', '/api/codex/launch-command', { ...ses, ...json }, '{"effort":"turbo"}'],
  ['POST', '/api/codex/launch-command', { ...ses, ...json }, '{"resumeSessionId":"bad"}'],
  { missing: ['codex-cli'] },
  ['POST', '/api/codex/launch-command', { ...ses, ...json }, '{}'],
  { missing: [] },
  ['POST', '/api/cli/tabs', { ...tok, ...json }, '{"workspaceId":"ws-b","panelType":"claude-code","prompt":"From the CLI","name":" Helper ","model":"sonnet"}'],
  ['POST', '/api/cli/tabs', { ...tok, ...json }, '{"workspaceId":"ws-b","panelType":"codex-cli","effort":"turbo"}'],
  ['POST', '/api/workspace', { ...ses, ...json }, `{"directory":"${C}","name":"Gamma","resumeSessionId":"${SID}"}`],
  ['POST', '/api/layout/pane/pane-a/tabs?workspace=ws-a', { ...ses, ...json }, `{"panelType":"codex-cli","resumeSessionId":"${SID2}","name":"Again"}`],
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length', 'x-accel-buffering'];
const UUID = /initial-[0-9a-f-]{36}\.txt/g;
// Claude names a project folder after its path with every other character a dash.
const flatRoot = root.replace(/[^a-zA-Z0-9]/g, '-');
const scrub = (s) => {
  let out = s.split(DATA).join('$DATA').split(root).join('$ROOT').split(flatRoot).join('$FLATROOT').replace(UUID, 'initial-UUID.txt');
  for (const [from, to] of NAMES) out = out.split(from).join(to);
  return out;
};
const out = [];
let lastEtag = '';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
for (const item of requests) {
  if (!Array.isArray(item)) {
    Object.assign(globalThis.__golden, item);
    out.push({ setup: item });
    continue;
  }
  const [method, url, headers, body] = item;
  const h = { ...headers };
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const before = globalThis.__golden.calls.length;
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: body === undefined ? h : { ...h, 'content-length': Buffer.byteLength(body) } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end(body);
  });
  if (url.includes('/workspace') || url.includes('/tabs?')) await sleep(800);
  if (res.headers.etag) lastEtag = res.headers.etag;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  out.push({
    method, url: scrub(url), headers: Object.fromEntries(Object.entries(headers).map(([k, v]) => [k === OLD_HEADER ? 'x-cli-token' : k, v])),
    body: body === undefined ? null : scrub(body), status: res.status, answerHeaders: kept, answer: scrub(res.text), scrubbed: scrub(res.text) !== res.text,
    calls: globalThis.__golden.calls.slice(before).map((c) => c.map((v) => scrub(String(v)))),
  });
}
server.close();

const before = globalThis.__golden.calls.length;
const resumed = await autoResumeForCore();
await sleep(1500);
const resume = { resumed, calls: globalThis.__golden.calls.slice(before).map((c) => c.map((v) => scrub(String(v)))) };

const left = {};
const walk = (dir) => {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p);
    else left[scrub(path.relative(DATA, p))] = /\/stop$|codex-launcher\.js$|bin\//.test(p) ? '' : scrub(fs.readFileSync(p, 'utf8'));
  }
};
walk(DATA);
const fixtures = Object.fromEntries(Object.entries(files).map(([k, v]) => [k, scrub(v)]));
const outsideFiles = Object.fromEntries(Object.entries(outside).map(([k, v]) => [scrub(k), scrub(v)]));
const result = JSON.stringify({ token: TOKEN, port: 18123, sid: SID, fixtures, outside: outsideFiles, requests: out, resume, files: left }, null, 1) + '\n';
for (const [from] of NAMES) if (result.includes(from)) throw new Error(`a product name is left: ${from}`);
if (new RegExp(process.env.OLD_MODULE_PREFIX, 'i').test(result)) throw new Error('a product name is left');
const target = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'launch-routes-golden.json');
fs.writeFileSync(target, result);
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests, ${resume.calls.length} resume calls → ${target}`);
process.exit(0);
