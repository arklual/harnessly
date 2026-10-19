// Sends requests to the previous runtime's own API dispatcher and its subagents handler, on fixture transcripts, and
// writes what each answered to ../golden.json. The Go test lays out the same files and must answer byte for byte.
//
// The previous runtime's header names and route prefix are not spelled here: they come from the caller, and the
// golden file names them neutrally (x-cli-token, x-old-cli-token, /api/platform).
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_MODULE_PREFIX=<prefix of its module names> node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated subagent transcripts and their summaries instead (TS_GOLDEN
// of the Go test).
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

if (!process.env.OLD_RUNTIME_NODE_MODULES) throw new Error('OLD_RUNTIME_NODE_MODULES is required (the diff package)');
process.env.TZ = 'UTC';
const FIXED = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'subagents-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const here = path.dirname(new URL(import.meta.url).pathname);

const M = process.env.OLD_RUNTIME_MODULE_PREFIX;
if (!M) throw new Error('OLD_RUNTIME_MODULE_PREFIX is required (the prefix of its own module names)');
const { summarizeSubagentTranscript } = await import(`@/lib/${M}subagents`);

// --- generated summaries --------------------------------------------------------------------------------------------
if (process.env.GEN_RANDOM) {
  let seed = 7;
  const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  const texts = ['', ' ', 'Find the bug', 'line one\nline two', 'Готово', 'x'.repeat(90), 'tab\there', '  padded  ', 'a\r\nb'];
  const tools = [
    () => ({ type: 'tool_use', id: 't' + Math.floor(rnd() * 99), name: 'Read', input: { file_path: pick(['/srv/a.go', 'b.txt']) } }),
    () => ({ type: 'tool_use', name: 'Bash', input: { command: pick(['ls -la', 'go test ./...\necho done', '']) } }),
    () => ({ type: 'tool_use', name: 'Edit', input: { file_path: '/srv/x.ts', old_string: pick(['', 'a\nb\n', 'one']), new_string: pick(['a\nc\n', 'two\nthree', '']) } }),
    () => ({ type: 'tool_use', name: 'Grep', input: { pattern: pick(['TODO', 'a"b']) } }),
    () => ({ type: 'tool_use', name: pick(['WebFetch', 'Task', 'Write', 'Glob']), input: pick([{ url: 'https://example.com/' + 'p'.repeat(70) }, {}, { file_path: '/t', content: 'c' }, { pattern: '*.go' }]) }),
    () => ({ type: 'tool_use', input: { a: 1 } }),
    () => ({ type: 'tool_use', name: null, input: null }),
    () => ({ type: 'tool_use', name: 'Agent', input: 'raw-input' }),
  ];
  const stamp = () => pick(['2026-10-17T23:50:00.000Z', '2026-10-17T23:59:30Z', 'not a date', '', undefined, '2026-10-17T10:00:00.250Z']);
  const line = () => {
    const k = Math.floor(rnd() * 12);
    const ts = stamp();
    const base = ts === undefined ? {} : { timestamp: ts };
    switch (k) {
      case 0: return JSON.stringify({ ...base, type: 'user', message: { role: 'user', content: pick(texts) } });
      case 1: return JSON.stringify({ ...base, type: 'user', message: { content: [{ type: 'text', text: pick(texts) }, { type: 'text' }, { type: 'image' }] } });
      case 2: return JSON.stringify({ ...base, type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 't1', content: 'ok' }] } });
      case 3: return JSON.stringify({ ...base, type: 'assistant', message: { content: [pick(tools)()], stop_reason: pick([null, 'tool_use', 'end_turn']) } });
      case 4: return JSON.stringify({ ...base, type: 'assistant', message: { content: [{ type: 'text', text: pick(texts) }], stop_reason: pick(['end_turn', null, 'max_tokens']) } });
      case 5: return JSON.stringify({ ...base, type: 'assistant', message: { content: [{ type: 'text', text: pick(texts) }, pick(tools)(), pick(tools)()], stop_reason: 'end_turn' } });
      case 6: return pick(['', '   ', '{broken', '[]', '5', '"text"', '{"type":"assistant","message":"flat"}', '{"type":"assistant","message":{"content":"str"}}']);
      case 7: return JSON.stringify({ ...base, type: 'system', subtype: 'x' });
      case 8: return JSON.stringify({ ...base, type: 'assistant', message: { content: [{ type: 'text', text: 7 }, { type: 'text', text: null }], stop_reason: 'end_turn' } });
      case 9: return rnd() < 0.1 ? 'null' : JSON.stringify({ ...base, type: 'user', message: null });
      default: return JSON.stringify({ ...base, type: pick(['user', 'assistant']), message: { content: [{ type: 'text', text: pick(texts) + pick(texts) }] } });
    }
  };
  const metas = [null, '{}', 'null', '{bad', JSON.stringify({ agentType: 'Explore', description: 'Look', toolUseId: 'toolu_1', requestShape: 'background' }),
    JSON.stringify({ agentType: '', description: 0, toolUseId: null }), JSON.stringify({ description: 5, agentType: ['a'] }), '"str"', '7'];
  const cases = [];
  for (let i = 0; i < Number(process.env.GEN_RANDOM); i++) {
    const lines = Array.from({ length: Math.floor(rnd() * 8) }, line);
    const raw = lines.join(pick(['\n', '\r\n'])) + pick(['', '\n']);
    const metaRaw = pick(metas);
    let meta = {};
    try { meta = JSON.parse(metaRaw ?? '{}'); } catch {}
    const mtimeMs = FIXED - pick([0, 1000, 44999, 45000, 45001, 600000]) + pick([0, 0.25]);
    let result = null;
    try { result = summarizeSubagentTranscript(raw, meta, 'id' + i, mtimeMs); } catch {}
    cases.push({ raw, metaRaw, mtimeMs, now: FIXED, result });
  }
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ cases }));
  fs.rmSync(root, { recursive: true, force: true });
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  process.exit(0);
}

// --- the route --------------------------------------------------------------------------------------------------------
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
if (OLD_HEADERS.length !== 2 || !OLD_API) throw new Error('OLD_RUNTIME_TOKEN_HEADERS (two names) and OLD_RUNTIME_PLATFORM_API are required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];
const NEUTRAL_API = '/api/platform';
const TOKEN = 'b'.repeat(64);

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
fs.writeFileSync(path.join(dataDir(), 'cli-token'), TOKEN, { mode: 0o600 });

const sec = FIXED / 1000;
const SESSION = 'home/.claude/projects/-srv-app/3f0c9a52-6d1e-4b7a-9c2d-0e1f2a3b4c5d';
const SUB = `${SESSION}/subagents`;
const j = (rows) => rows.map((r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n';
const files = [
  { path: `${SESSION}.jsonl`, content: j([{ type: 'user', message: { content: 'hi' } }]), mtime: sec - 5 },
  { path: 'home/.claude/projects/-srv-app/empty-session.jsonl', content: '', mtime: sec - 5 },
  { path: 'home/.codex/sessions/2026/10/17/rollout-1.jsonl', content: '{}\n', mtime: sec - 5 },
  { path: `${SUB}/agent-a1.jsonl`, mtime: sec - 3600.75, content: j([
    { type: 'user', timestamp: '2026-10-17T22:00:00.000Z', message: { content: 'Find the bug\nin the parser' } },
    { type: 'assistant', timestamp: '2026-10-17T22:00:05.000Z', message: { content: [{ type: 'text', text: 'Looking' }, { type: 'tool_use', id: 'toolu_a', name: 'Read', input: { file_path: '/srv/app/parser.go' } }], stop_reason: 'tool_use' } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'toolu_a', content: 'ok' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: '  Fixed: the loop ran one past the end.  ' }], stop_reason: 'end_turn' } },
  ]) },
  { path: `${SUB}/agent-a1.meta.json`, mtime: sec - 3600, content: JSON.stringify({ agentType: 'Explore', description: 'Look for the bug', toolUseId: 'toolu_parent' }) },
  { path: `${SUB}/agent-b2.jsonl`, mtime: sec - 10.25, content: j([
    { type: 'user', timestamp: '2026-10-17T23:59:00Z', message: { content: [{ type: 'text', text: 'Write tests' }, { type: 'image' }, { type: 'text', text: 'for the cache' }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'go test ./...\necho done' } }], stop_reason: 'tool_use' } },
  ]) },
  { path: `${SUB}/agent-b2.meta.json`, mtime: sec - 10, content: JSON.stringify({ requestShape: 'background', agentType: 'general-purpose' }) },
  { path: `${SUB}/agent-c3.jsonl`, mtime: sec - 600, content: j([
    'not json', '',
    { type: 'user', timestamp: 'yesterday', message: { content: 'Long task: ' + 'x'.repeat(7000) } },
    { type: 'assistant', timestamp: '2026-10-17T21:00:00Z', message: { content: [{ type: 'text', text: 'partial answer' }], stop_reason: null } },
  ]) },
  { path: `${SUB}/agent-d4.jsonl`, mtime: sec - 600, content: j([{ type: 'user', message: { content: 'x' } }, 'null']) },
  { path: `${SUB}/agent-e5.jsonl`, mtime: sec - 600, content: j([{ type: 'user', message: { content: 'meta is null' } }]) },
  { path: `${SUB}/agent-e5.meta.json`, mtime: sec - 600, content: 'null' },
  { path: `${SUB}/agent-f6.jsonl`, mtime: sec - 900, content: j([
    { type: 'user', timestamp: '2026-10-17T20:00:00.000Z', message: { content: [{ type: 'text', text: '' }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Edit', input: { file_path: '/srv/app/a.ts', old_string: 'one\ntwo\n', new_string: 'one\n2\nthree\n' } }], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'WebFetch', input: { url: 'https://example.com/' + 'p'.repeat(80), prompt: 'x' } }, { type: 'tool_use', input: { q: 1 } }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'All done' }], stop_reason: 'end_turn' } },
  ]) },
  { path: `${SUB}/agent-f6.meta.json`, mtime: sec - 900, content: '{not json' },
  { path: `${SUB}/agent-g7.jsonl`, mtime: sec - 7200, content: '' },
  { path: `${SUB}/agent-h8.jsonl`, mtime: sec - 30, content: String.fromCharCode(0xfeff) + j([{ type: 'user', timestamp: '2026-10-17T20:00:00.000Z', message: { content: 'bom first line' } }]).replace(/\n/g, '\r\n') + j([
    { type: 'user', timestamp: '2026-10-17T20:00:00.000Z', message: { content: 'second prompt' } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn' } },
  ]) },
  { path: `${SUB}/agent-i9.jsonl`, mtime: sec - 30, content: j([
    { type: 'user', timestamp: '2026-10-17T20:00:00.000Z', message: { content: 'same start as h8' } },
  ]) },
  { path: `${SUB}/agent-i9.meta.json`, mtime: sec - 30, content: JSON.stringify({ description: 42, agentType: ['x', null], toolUseId: { id: 1 } }) },
  { path: `${SUB}/agent-j10.jsonl`, mtime: sec - 30, content: j([
    { type: 'user', isSidechain: true, uuid: 'u1', parentUuid: null, sessionId: 's', timestamp: '2026-10-17T20:00:00.000Z', message: { role: 'user', content: 'Count the files' } },
    { type: 'assistant', isSidechain: true, uuid: 'u2', parentUuid: 'u1', timestamp: '2026-10-17T20:00:01.000Z', message: { id: 'm1', role: 'assistant', model: 'claude-x', content: [{ type: 'thinking', thinking: 'hm' }, { type: 'text', text: 'Counting' }, { type: 'tool_use', id: 'toolu_1', name: 'Bash', input: { command: 'ls | wc -l', description: 'count' } }], stop_reason: 'tool_use', usage: { input_tokens: 3, output_tokens: 5 } } },
    { type: 'user', isSidechain: true, uuid: 'u3', parentUuid: 'u2', timestamp: '2026-10-17T20:00:02.000Z', message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'toolu_1', content: '12', is_error: false }] }, toolUseResult: { stdout: '12', stderr: '' } },
    { type: 'assistant', isSidechain: true, uuid: 'u4', parentUuid: 'u3', timestamp: '2026-10-17T20:00:03.000Z', message: { id: 'm2', role: 'assistant', content: [{ type: 'tool_use', id: 'toolu_2', name: 'Read', input: { file_path: '/srv/app/a.go' } }], stop_reason: 'tool_use' } },
    { type: 'user', isSidechain: true, uuid: 'u5', parentUuid: 'u4', timestamp: '2026-10-17T20:00:04.000Z', message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'toolu_2', content: [{ type: 'text', text: 'package a' }] }] } },
    { type: 'assistant', isSidechain: true, uuid: 'u6', parentUuid: 'u5', timestamp: '2026-10-17T20:00:05.000Z', message: { id: 'm3', role: 'assistant', content: [{ type: 'text', text: 'There are 12 files.' }], stop_reason: 'end_turn' } },
  ]) },
  { path: `${SUB}/agent-k11.jsonl`, mtime: sec - 30, content: j([
    { type: 'user', uuid: 'v1', timestamp: '2026-10-17T20:00:00.000Z', message: { role: 'user', content: 'not a sidechain' } },
    { type: 'assistant', uuid: 'v2', timestamp: '2026-10-17T20:00:01.000Z', message: { role: 'assistant', content: [{ type: 'text', text: 'plain' }] } },
    { type: 'user', isSidechain: true, uuid: 'v3', timestamp: '2026-10-17T20:00:02.000Z', message: { role: 'user', content: 'then a sidechain' } },
    'broken {',
    { type: 'summary', summary: 'A summary' },
  ]) },
  { path: `${SUB}/not-an-agent.jsonl`, mtime: sec - 30, content: j([{ type: 'user', message: { content: 'ignored' } }]) },
  { path: `${SUB}/agent-bad name.jsonl`, mtime: sec - 30, content: j([{ type: 'user', message: { content: 'ignored' } }]) },
];
for (const f of files) {
  const file = path.join(root, f.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, f.content);
  fs.utimesSync(file, f.mtime, f.mtime);
}

const dispatch = createApiDispatcher([
  { route: `${OLD_API}/subagents`, load: () => import(`@/pages${OLD_API}/subagents/index`) },
  { route: `${OLD_API}/subagents/[id]`, load: () => import(`@/pages${OLD_API}/subagents/[id]`) },
]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const ses = { cookie: 'session-token=good' };
const tok = { 'x-cli-token': TOKEN };
const P = (p) => `$ROOT/${p}`;
const requests = [
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], {}],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], tok],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], { 'x-cli-token': 'wrong', 'x-old-cli-token': TOKEN }],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], { 'x-old-cli-token': TOKEN, ...ses }],
  ['POST', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['DELETE', '/api/platform/subagents', [], ses],
  ['GET', '/api/platform/subagents', [], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', '']], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)], ['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/elsewhere/s.jsonl')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.json`)]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/.claude/projects/../../etc/s.jsonl')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/.claude/projects')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/.codex/sessions/2026/10/17/rollout-1.jsonl')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/.claude/projects/-srv-app/empty-session.jsonl')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P('home/.claude/projects/-srv-app/missing.jsonl')]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents', [['jsonlPath', P(`${SESSION}.jsonl`)]], { ...ses, 'if-none-match': 'ETAG' }],
  ['GET', '/agents/api/platform/subagents/', [['jsonlPath', P(`home/.claude/projects/../projects/-srv-app/3f0c9a52-6d1e-4b7a-9c2d-0e1f2a3b4c5d.jsonl`)]], ses],
  // One subagent's transcript.
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)]], {}],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)]], tok],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)]], { ...tok, ...ses }],
  ['POST', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/a1', [], ses],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)], ['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P('home/elsewhere/s.jsonl')]], ses],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P('home/.codex/sessions/2026/10/17/rollout-1.jsonl')]], ses],
  ['GET', '/api/platform/subagents/bad%20name', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/a1.meta', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/..%2Fx', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/missing', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform/subagents/b2', [['jsonlPath', P('home/.claude/projects/-srv-app/missing.jsonl')]], ses],
  ...['a1', 'b2', 'c3', 'd4', 'e5', 'f6', 'g7', 'h8', 'i9', 'j10', 'k11'].map((id) => ['GET', `/api/platform/subagents/${id}`, [['jsonlPath', P(`${SESSION}.jsonl`)]], ses]),
  ['GET', '/api/platform/subagents/j10', [['jsonlPath', P(`${SESSION}.jsonl`)]], { ...ses, 'if-none-match': 'ETAG' }],
  ['GET', '/api/platform/subagents/a1', [['jsonlPath', P(`${SESSION}.jsonl`)], ['id', 'b2']], ses],
  ['GET', '/agents/api/platform/subagents/j10/', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
  ['GET', '/api/platform//subagents//a1', [['jsonlPath', P(`${SESSION}.jsonl`)]], ses],
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const scrub = (s) => s.split(root).join('$ROOT');
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
// The handler keeps summaries between requests; each request here starts without them, as the Go test does.
const subagentCacheClear = () => globalThis.__ptSubagentCache?.clear();
const out = [];
let lastEtag = '';
for (const [method, urlPath, query, headers] of requests) {
  subagentCacheClear();
  const h = toOld({ ...headers });
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v.replace('$ROOT', root))}`).join('&');
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
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  out.push({ method, path: urlPath, query, headers, status: res.status, answerHeaders: kept, answer: scrub(res.text) });
}
server.close();
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify({ now: FIXED, token: TOKEN, files, requests: out }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests → ${path.join(here, '..', 'golden.json')}`);
