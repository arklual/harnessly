// Sends requests to the previous runtime's own API dispatcher and its economics handler, on fixture layouts and
// transcripts, and writes what each answered, and the cursor files it left, to ../golden.json. The Go test lays out the
// same files, sends the same requests and must answer byte for byte. The machine's limits are a fixed stand-in on both
// sides (they are the machine snapshot's, tested there).
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_MODULE_PREFIX=<prefix of its module names> \
//   OLD_RUNTIME_META_KEY=<its tab metadata key> node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated transcripts, the cursor each leaves and the economics summed
// from them instead (TS_GOLDEN of the Go test).
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

process.env.TZ = 'UTC';
const FIXED = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
const M = process.env.OLD_RUNTIME_MODULE_PREFIX;
if (!M) throw new Error('OLD_RUNTIME_MODULE_PREFIX is required');

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'economics-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const here = path.dirname(new URL(import.meta.url).pathname);
globalThis.__golden = {
  limits: {
    claude: { fiveHour: { usedPct: 41, resetsAt: 1792290000, sampledAt: 1792280000, source: null, forecast: { status: 'stale' }, pace: null }, sevenDay: null, updatedAt: 1792280000000, plan: 'Max', error: null, extra: [] },
    codex: null,
  },
};

const econ = await import(`@/lib/${M}economics`);

// --- generated transcripts ------------------------------------------------------------------------------------------
if (process.env.GEN_RANDOM) {
  let seed = 11;
  const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  const int = (n) => Math.floor(rnd() * n);
  const count = () => pick([0, 0, 1, 5, 120, 3000, 45678, 1.5, -3, '7', null, undefined, 1e6]);
  const claudeModels = ['claude-opus-4-6', 'claude-opus-4-6-20251001', 'claude-sonnet-4-5[1m]', 'claude-haiku-4-5-20251001', 'claude-opus-5-5',
    'claude-fable-5-1', 'claude-3-5-sonnet', 'claude-sonnet-4-v1:0', 'us.claude-opus-4-1', 'claude-unknown-9', '', 'synthetic', 7, null, 'claude-opus-4'];
  const codexModels = ['gpt-5.4', 'gpt-5.4-mini', 'GPT-5.1-Codex', 'openai/gpt-5', 'gpt-5-2026-01-01', 'gpt-5.2-codex-20260101', 'gpt-4o-mini',
    'o3', '', ' gpt-5.5 ', 'gpt-5.1-codex-max-high', 'models/gpt-4.1'];
  const stamp = () => pick(['2026-10-17T10:00:00.000Z', '2026-10-17T10:05:30Z', '2026-10-16T23:59:59.999Z', 'bad', 5, undefined, '2026-10-17']);
  const ids = ['msg_1', 'msg_2', 'msg_3', '', 0, 12, true, null];
  const claudeLine = () => {
    switch (int(9)) {
      case 0: case 1: case 2: {
        const usage = { input_tokens: count(), output_tokens: count(), cache_read_input_tokens: count(), cache_creation_input_tokens: count() };
        if (rnd() < 0.5) usage.cache_creation = pick([{ ephemeral_5m_input_tokens: count(), ephemeral_1h_input_tokens: count() }, {}, null, 'x']);
        if (rnd() < 0.3) usage.output_tokens_details = pick([{ thinking_tokens: count() }, {}, null]);
        const message = { id: pick(ids), model: pick(claudeModels), usage: rnd() < 0.05 ? pick([null, 0, 'u']) : usage };
        if (rnd() < 0.2) delete message.id;
        return JSON.stringify({ type: 'assistant', timestamp: stamp(), message });
      }
      case 3: return JSON.stringify({ type: 'system', subtype: pick(['turn_duration', 'other']), durationMs: count(), timestamp: stamp() });
      case 4: return JSON.stringify({ type: 'cost-state', totalCostUSD: pick([0.42, 0, 1.5, -1, '2', null]) });
      case 5: return JSON.stringify({ type: 'user', message: { content: 'assistant said "assistant"' } });
      case 6: return pick(['{"type":"assistant"', '"assistant"', '["assistant"]', 'null', '{"type":"assistant","message":"assistant"}', '']);
      case 7: return JSON.stringify({ type: 'assistant', message: { content: 'x'.repeat(int(300)) } });
      default: return JSON.stringify({ type: 'progress', data: 'turn_duration' });
    }
  };
  const codexLine = (st) => {
    switch (int(8)) {
      case 0: return JSON.stringify({ type: 'turn_context', payload: { model: pick(codexModels) } });
      case 1: case 2: case 3: {
        if (rnd() < 0.15) st.c = { i: 0, c: 0, o: 0, r: 0 };
        st.c.i += int(5000); st.c.c += int(3000); st.c.o += int(800); st.c.r += int(200);
        const total = { input_tokens: st.c.i, cached_input_tokens: Math.min(st.c.c, st.c.i + 50), output_tokens: st.c.o, reasoning_output_tokens: st.c.r };
        return JSON.stringify({ type: 'event_msg', timestamp: stamp(), payload: { type: 'token_count', info: rnd() < 0.1 ? null : { total_token_usage: total } } });
      }
      case 4: return JSON.stringify({ type: 'event_msg', timestamp: stamp(), payload: { type: 'task_complete', duration_ms: count() } });
      case 5: return JSON.stringify({ type: 'turn_context', payload: { cwd: '/srv', model: 5 } });
      case 6: return pick(['{"type":"event_msg","payload":null,"x":"token_count"}', 'token_count', '{"type":"event_msg","payload":"task_complete"}']);
      default: return JSON.stringify({ type: 'response_item', payload: { type: 'message', text: 'turn_context' } });
    }
  };
  const transcript = (provider) => {
    const st = { c: { i: 0, c: 0, o: 0, r: 0 } };
    const lines = Array.from({ length: int(14) }, () => (provider === 'claude' ? claudeLine() : codexLine(st)));
    const text = lines.join('\n') + pick(['\n', '', '\n{"type":"assistant","message":{"id":"tail"']);
    const chunks = [];
    for (let at = 0; at < text.length;) {
      const n = 1 + int(Math.max(1, text.length / 2));
      chunks.push(text.slice(at, at + n));
      at += n;
    }
    const state = econ.createFileUsageState('/t/' + provider + '.jsonl', provider);
    for (const c of chunks) econ.consumeChunk(state, c);
    return { chunks, state };
  };
  const cases = [];
  for (let i = 0; i < Number(process.env.GEN_RANDOM); i++) {
    const provider = pick(['claude', 'codex']);
    const main = transcript(provider);
    const subs = provider === 'claude' ? Array.from({ length: int(3) }, () => transcript('claude')) : [];
    const source = { provider, sessionId: pick(['3f0c9a52-6d1e-4b7a-9c2d-0e1f2a3b4c5d', null, undefined]), jsonlPath: '/t/x.jsonl' };
    const statuslineCost = pick([null, 0.75, 0, '3']);
    const summary = econ.summarizeConversation(source, main.state, subs.map((s) => s.state), statuslineCost);
    cases.push({ provider, sessionId: source.sessionId === undefined ? '$undefined' : source.sessionId, statuslineCost, chunks: main.chunks,
      subChunks: subs.map((s) => s.chunks), state: JSON.stringify(main.state), subStates: subs.map((s) => JSON.stringify(s.state)),
      summary: JSON.stringify(summary) });
  }
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ now: FIXED, cases }));
  fs.rmSync(root, { recursive: true, force: true });
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  process.exit(0);
}

// --- the route --------------------------------------------------------------------------------------------------------
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
const META = process.env.OLD_RUNTIME_META_KEY;
if (OLD_HEADERS.length !== 2 || !OLD_API || !META) throw new Error('OLD_RUNTIME_TOKEN_HEADERS (two names), OLD_RUNTIME_PLATFORM_API and OLD_RUNTIME_META_KEY are required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];
const TOKEN = 'c'.repeat(64);

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const DATA = dataDir();
fs.writeFileSync(path.join(DATA, 'cli-token'), TOKEN, { mode: 0o600 });

const SID = '3f0c9a52-6d1e-4b7a-9c2d-0e1f2a3b4c5d';
const SID2 = '0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d';
const PROJ = 'home/.claude/projects/-srv-app';
const CODEX = 'home/.codex/sessions/2026/10/17';
const j = (rows) => rows.map((r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n';
const assistant = (id, model, usage, ts = '2026-10-17T10:00:00.000Z') => ({ type: 'assistant', timestamp: ts, message: { id, model, role: 'assistant', usage } });
const claudeMain = j([
  { type: 'user', timestamp: '2026-10-17T09:59:00.000Z', message: { content: 'start' } },
  assistant('msg_1', 'claude-opus-4-6', { input_tokens: 10, output_tokens: 200, cache_read_input_tokens: 5000, cache_creation_input_tokens: 3000,
    cache_creation: { ephemeral_5m_input_tokens: 1000, ephemeral_1h_input_tokens: 2000 } }),
  assistant('msg_1', 'claude-opus-4-6', { input_tokens: 10, output_tokens: 200, cache_read_input_tokens: 5000, cache_creation_input_tokens: 3000 }),
  assistant('msg_2', 'claude-sonnet-4-5-20250929', { input_tokens: 7, output_tokens: 90, cache_read_input_tokens: 0, cache_creation_input_tokens: 400,
    output_tokens_details: { thinking_tokens: 30 } }, '2026-10-17T10:02:00.000Z'),
  { type: 'system', subtype: 'turn_duration', durationMs: 120000, timestamp: '2026-10-17T10:03:00.000Z' },
  { type: 'user', message: { content: 'x'.repeat(5000) } },
  assistant(undefined, 'claude-opus-4-6', { input_tokens: 0, output_tokens: 0 }, '2026-10-17T08:00:00.000Z'),
]) + '{"type":"assistant","timestamp":"2026-10-17T10:04:00.000Z","message":{"id":"msg_3","model":"claude-opus-4-6","usage":{"input_tokens":3,';
const claudeMore = '"output_tokens":40}}}\n' + j([
  { type: 'cost-state', totalCostUSD: 0.4215 },
  { type: 'system', subtype: 'turn_duration', durationMs: 30000, timestamp: '2026-10-17T10:05:00.000Z' },
]);
const files = [
  { path: `${PROJ}/${SID}.jsonl`, content: claudeMain },
  { path: `${PROJ}/${SID}/subagents/agent-s1.jsonl`, content: j([assistant('msg_s1', 'claude-haiku-4-5-20251001', { input_tokens: 100, output_tokens: 50, cache_read_input_tokens: 900 }, '2026-10-17T10:01:00.000Z')]) },
  { path: `${PROJ}/${SID}/subagents/agent-s2.jsonl`, content: j([assistant('msg_s2', 'claude-opus-4-6', { input_tokens: 1, output_tokens: 2 }, '2026-10-17T09:30:00.000Z')]) },
  { path: `${PROJ}/${SID}/subagents/agent-s2.meta.json`, content: '{}' },
  { path: `${PROJ}/${SID}/subagents/notes.jsonl`, content: j([assistant('msg_n', 'claude-opus-4-6', { input_tokens: 1000000 })]) },
  { path: `${PROJ}/${SID2}.jsonl`, content: j([assistant('m1', 'claude-unreleased-9', { input_tokens: 5, output_tokens: 5 })]) },
  { path: `${PROJ}/plain.jsonl`, content: j([assistant('m1', 'claude-haiku-4-5', { input_tokens: 2000, output_tokens: 100 }), { type: 'system', subtype: 'turn_duration', durationMs: 5 }]) },
  { path: `${PROJ}/cursor.jsonl`, content: j([
    assistant('c1', 'claude-opus-4-6', { input_tokens: 1, output_tokens: 1 }),
    assistant('c2', 'claude-opus-4-6', { input_tokens: 1, output_tokens: 1 }, '2026-10-17T11:00:00.000Z'),
  ]) },
  { path: `${PROJ}/replaced.jsonl`, content: j([assistant('r1', 'claude-opus-4-6', { input_tokens: 1000, output_tokens: 10 })]) },
  { path: `${CODEX}/rollout-a.jsonl`, content: j([
    { type: 'session_meta', payload: { id: 'c-1' } },
    { type: 'turn_context', payload: { model: 'gpt-5.4' } },
    { type: 'event_msg', timestamp: '2026-10-17T12:00:00.000Z', payload: { type: 'token_count', info: null } },
    { type: 'event_msg', timestamp: '2026-10-17T12:00:01.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 10000, cached_input_tokens: 6000, output_tokens: 500, reasoning_output_tokens: 100 } } } },
    { type: 'event_msg', timestamp: '2026-10-17T12:01:00.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 25000, cached_input_tokens: 20000, output_tokens: 900, reasoning_output_tokens: 150 } } } },
    { type: 'event_msg', timestamp: '2026-10-17T12:02:00.000Z', payload: { type: 'task_complete', duration_ms: 115000 } },
    { type: 'turn_context', payload: { model: 'gpt-5.4-mini' } },
    { type: 'event_msg', timestamp: '2026-10-17T12:05:00.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 3000, cached_input_tokens: 1000, output_tokens: 100 } } } },
  ]) },
  { path: `${CODEX}/rollout-b.jsonl`, content: j([
    { type: 'turn_context', payload: { model: 'o9-preview' } },
    { type: 'event_msg', timestamp: '2026-10-17T12:00:01.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 100, output_tokens: 5 } } } },
  ]) },
];
const write = (f) => {
  const file = path.join(root, f.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, f.content);
};
files.forEach(write);
const tab = (id, agentState, extra = {}) => ({ id, sessionName: `hs-ws-a-pane-a-${id}`, name: id, order: 0, cwd: '/srv/app', ...(agentState === undefined ? {} : { agentState }), ...extra });
const P = (p) => path.join(root, p);
const fixtures = {
  'workspaces.json': JSON.stringify({ workspaces: [{ id: 'ws-a', name: 'A', directories: ['/srv/app'] }, { id: 'ws-b', name: 'B', directories: ['/srv/b'] }], groups: [] }, null, 2),
  'workspaces/ws-a/layout.json': JSON.stringify({ root: { type: 'split', id: 's', orientation: 'horizontal', ratio: 50, children: [
    { type: 'pane', id: 'pane-a', tabs: [
      tab('tab-claude', { providerId: 'claude', sessionId: SID, jsonlPath: P(`${PROJ}/${SID}.jsonl`), summary: null }, { [META]: { launch: { model: 'opus', effort: 'high' } } }),
      tab('tab-shell'),
      tab('tab-other', { providerId: 'gemini', sessionId: 'g', jsonlPath: P(`${PROJ}/plain.jsonl`) }),
      tab('tab-flat', 'claude'),
      tab('tab-nopath', { providerId: 'claude', sessionId: SID2, jsonlPath: null }),
      tab('tab-missing', { providerId: 'claude', sessionId: SID2, jsonlPath: P(`${PROJ}/missing.jsonl`) }),
    ] },
    { type: 'pane', id: 'pane-b', tabs: [
      tab('tab-nosession', { providerId: 'claude', jsonlPath: P(`${PROJ}/plain.jsonl`) }),
      tab('tab-statscost', { providerId: 'claude', sessionId: SID2, jsonlPath: P(`${PROJ}/${SID2}.jsonl`) }),
      tab('tab-cursor', { providerId: 'claude', sessionId: null, jsonlPath: P(`${PROJ}/cursor.jsonl`) }),
      tab('tab-replaced', { providerId: 'claude', sessionId: SID2, jsonlPath: P(`${PROJ}/replaced.jsonl`) }),
      tab('tab-codex', { providerId: 'codex', sessionId: 'c-1', jsonlPath: P(`${CODEX}/rollout-a.jsonl`) }),
      tab('tab-codex-unpriced', { providerId: 'codex', sessionId: 'c-2', jsonlPath: P(`${CODEX}/rollout-b.jsonl`) }),
      tab('tab-numpath', { providerId: 'codex', sessionId: 'c-3', jsonlPath: 5 }),
      tab('tab-numpath-claude', { providerId: 'claude', jsonlPath: 5 }),
    ] },
  ] } }, null, 2),
  'workspaces/ws-b/layout.json': JSON.stringify({ root: { type: 'pane', id: 'pane-x', tabs: [tab('tab-claude', { providerId: 'codex', sessionId: 'dup' })] } }, null, 2),
  [`session-stats/${SID2}.json`]: JSON.stringify({ sessionId: SID2, cost: 1.25, model: 'Opus' }),
};
const scrub = (s) => s.split(root).join('$ROOT');
for (const [name, content] of Object.entries(fixtures)) {
  fs.mkdirSync(path.dirname(path.join(DATA, name)), { recursive: true });
  fs.writeFileSync(path.join(DATA, name), content);
}
// A cursor the old runtime left for cursor.jsonl after its first line, its counters marked so the test sees them used.
const cursorFile = P(`${PROJ}/cursor.jsonl`);
const firstLine = fs.readFileSync(cursorFile, 'utf8').split('\n')[0] + '\n';
const st = fs.statSync(cursorFile);
const cursor = { ...econ.createFileUsageState(cursorFile, 'claude'), dev: st.dev, ino: st.ino };
econ.consumeChunk(cursor, firstLine);
cursor.offset = Buffer.byteLength(firstLine);
cursor.turns = 7;
const cursorText = JSON.stringify(cursor);
const sha = (p) => crypto.createHash('sha1').update(p).digest('hex');
fs.mkdirSync(econ.ECONOMICS_DIR, { recursive: true });
fs.writeFileSync(path.join(econ.ECONOMICS_DIR, `${sha(cursorFile)}.json`), cursorText);
const neutralCursor = (text) => scrub(text).replace(/"dev":\d+,"ino":\d+/, '"dev":"$DEV","ino":"$INO"');

const dispatch = createApiDispatcher([{ route: `${OLD_API}/economics`, load: () => import(`@/pages${OLD_API}/economics`) }]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const ses = { cookie: 'session-token=good' };
const tok = { 'x-cli-token': TOKEN };
const E = '/api/platform/economics';
// Steps: a request, or an action on the files between requests (the Go test does the same).
const steps = [
  { req: ['GET', E, [['tab', 'tab-claude']], {}] },
  { req: ['GET', E, [['tab', 'tab-claude']], tok] },
  { req: ['POST', E, [['tab', 'tab-claude']], ses] },
  { req: ['GET', E, [], ses] },
  { req: ['GET', E, [['tab', 'bad id!']], ses] },
  { req: ['GET', E, [['tab', 'x'.repeat(129)]], ses] },
  { req: ['GET', E, [['tab', 'tab-claude'], ['tab', 'tab-claude']], ses] },
  { req: ['GET', E, [['tab', 'tab-unknown']], ses] },
  { req: ['GET', E, [['tab', 'tab-shell']], ses] },
  { req: ['GET', E, [['tab', 'tab-other']], ses] },
  { req: ['GET', E, [['tab', 'tab-flat']], ses] },
  { req: ['GET', E, [['tab', 'tab-claude']], ses] },
  { req: ['GET', E, [['tab', 'tab-claude']], { ...ses, 'if-none-match': 'ETAG' }] },
  { append: `${PROJ}/${SID}.jsonl`, content: claudeMore },
  { req: ['GET', E, [['tab', 'tab-claude']], ses] },
  { req: ['GET', E, [['tab', 'tab-claude'], ['fresh', '0']], ses] },
  { req: ['GET', E, [['tab', 'tab-claude'], ['fresh', '1']], ses] },
  { req: ['GET', '/agents' + E + '/', [['tab', 'tab-claude'], ['fresh', '1']], ses] },
  { req: ['GET', E, [['tab', 'tab-nopath']], ses] },
  { req: ['GET', E, [['tab', 'tab-missing']], ses] },
  { req: ['GET', E, [['tab', 'tab-nosession']], ses] },
  { req: ['GET', E, [['tab', 'tab-statscost']], ses] },
  { req: ['GET', E, [['tab', 'tab-cursor']], ses] },
  { req: ['GET', E, [['tab', 'tab-replaced']], ses] },
  { replace: `${PROJ}/replaced.jsonl`, content: j([assistant('r2', 'claude-sonnet-4-5', { input_tokens: 5, output_tokens: 5 })]) },
  { req: ['GET', E, [['tab', 'tab-replaced'], ['fresh', '1']], ses] },
  { truncate: `${PROJ}/plain.jsonl`, content: j([assistant('m9', 'claude-haiku-4-5', { input_tokens: 1, output_tokens: 1 })]) },
  { req: ['GET', E, [['tab', 'tab-nosession'], ['fresh', '1']], ses] },
  { req: ['GET', E, [['tab', 'tab-codex']], { ...ses, 'x-cli-token': 'wrong' }] },
  { req: ['GET', E, [['tab', 'tab-codex-unpriced']], ses] },
  { req: ['GET', E, [['tab', 'tab-numpath']], ses] },
  { req: ['GET', E, [['tab', 'tab-numpath-claude']], ses] },
  { remove: `${PROJ}/${SID}.jsonl` },
  { req: ['GET', E, [['tab', 'tab-claude'], ['fresh', '1']], ses] },
  { req: ['GET', E, [['tab', 'tab-claude']], ses] },
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
const cursors = () => {
  const out = {};
  for (const name of fs.existsSync(econ.ECONOMICS_DIR) ? fs.readdirSync(econ.ECONOMICS_DIR) : []) {
    const text = fs.readFileSync(path.join(econ.ECONOMICS_DIR, name), 'utf8');
    out[scrub(JSON.parse(text).path)] = neutralCursor(text);
  }
  return out;
};
let seen = cursors();
const out = [];
let lastEtag = '';
for (const step of steps) {
  if (!step.req) {
    const file = P(step.append ?? step.replace ?? step.truncate ?? step.remove);
    if (step.append) fs.appendFileSync(file, step.content);
    if (step.replace) { fs.writeFileSync(file + '.new', step.content); fs.renameSync(file + '.new', file); }
    if (step.truncate) fs.writeFileSync(file, step.content);
    if (step.remove) fs.rmSync(file);
    out.push(step);
    continue;
  }
  const [method, urlPath, query, headers] = step.req;
  const h = toOld({ ...headers });
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
  const url = urlPath.replace('/api/platform', OLD_API) + (qs ? `?${qs}` : '');
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: h }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end();
  });
  if (res.headers.etag) lastEtag = res.headers.etag;
  await econ.flushEconomics();
  const now = cursors();
  const written = {};
  for (const [k, v] of Object.entries(now)) if (seen[k] !== v) written[k] = v;
  seen = now;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  out.push({ method, path: urlPath, query, headers, status: res.status, answerHeaders: kept, answer: scrub(res.text), cursorsWritten: written });
}
server.close();
// The price tables on every row and the forms of model ids met in transcripts.
const { calculateCost } = await import('@/lib/claude-tokens');
const { calculateOpenAICost } = await import('@/lib/openai-tokens');
const claudeIds = ['fable-5-1', 'fable-5', 'opus-5-5', 'opus-5', 'sonnet-5', 'opus-4-8', 'opus-4-7', 'opus-4-6', 'opus-4-5', 'opus-4-1', 'opus-4',
  'sonnet-4-6', 'sonnet-4-5', 'sonnet-4', 'sonnet-3-7', 'sonnet-3-5', 'haiku-4-5', 'haiku-3-5', 'haiku-3', 'opus-3']
  .flatMap((k) => [`claude-${k}`, `claude-${k}-20251001`, `claude-${k}[1m]`, `us.anthropic.claude-${k}-v1:0`])
  .concat(['claude-3-5-sonnet-20241022', 'claude-opus-4-6-2025', 'claude-opus-4-6-v2', 'opus-4-6', '', 'claude-x', 'claude-opus-4-6[1m][x]']);
const openaiIds = ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.4-nano', 'gpt-5.2-codex', 'gpt-5.2-chat-latest', 'gpt-5.2', 'gpt-5.1-codex-max',
  'gpt-5.1-codex', 'gpt-5.1-chat-latest', 'gpt-5.1', 'gpt-5-codex', 'gpt-5-chat-latest', 'gpt-5-mini', 'gpt-5-nano', 'gpt-5', 'gpt-4.1-mini',
  'gpt-4.1-nano', 'gpt-4.1', 'gpt-4o-mini', 'gpt-4o']
  .flatMap((k) => [k, k.toUpperCase(), `openai/${k}`, `models/${k}-2026-01-01`, `${k}-20260101`, `${k}-high`, ` ${k} `])
  .concat(['gpt-5.4o', 'gpt-6', 'o3', '', 'gpt-5-codex-mini-x']);
const counts = [[1234, 567, 8901, 2345, 67890], [1, 0, 0, 0, 3], [1e6, 1e6, 1e6, 1e6, 1e6]];
const pricing = {
  claude: claudeIds.flatMap((id) => counts.map((c) => ({ id, counts: c, cost: calculateCost(id, ...c) }))),
  openai: openaiIds.flatMap((id) => counts.map((c) => ({ id, counts: c, cost: calculateOpenAICost(id, c[0] + c[1], c[1], c[2]) }))),
};
const golden = {
  now: FIXED, token: TOKEN, limits: globalThis.__golden.limits, pricing,
  files: files.map((f) => ({ path: f.path, content: f.content })),
  data: Object.fromEntries(Object.entries(fixtures).map(([k, v]) => [k, scrub(v).split(`"${META}"`).join('"meta"')])),
  cursor: { path: `$ROOT/${PROJ}/cursor.jsonl`, content: neutralCursor(cursorText) },
  steps: out,
};
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify(golden, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} steps → ${path.join(here, '..', 'golden.json')}`);
