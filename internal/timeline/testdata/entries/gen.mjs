// Sends requests to the previous runtime's own API dispatcher and its transcript-page handler (timeline/entries) on
// generated Claude and Codex transcripts, and writes what each answered to ../entries.json. The Go test lays out the
// same files and must answer byte for byte; a request marked "stays" is one the Go route leaves to the old runtime.
//
// The previous runtime's header names are not spelled here: they come from the caller, and the golden file names them
// neutrally (x-cli-token, x-old-cli-token).
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_MODULE_PREFIX=<prefix of its module names> node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated transcripts and page reads instead (ENTRIES_RANDOM of the Go
// test).
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

if (!process.env.OLD_RUNTIME_NODE_MODULES) throw new Error('OLD_RUNTIME_NODE_MODULES is required (zod and diff)');
process.env.TZ = 'UTC';
const FIXED = Date.parse('2026-10-18T12:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'entries-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const here = path.dirname(new URL(import.meta.url).pathname);

let seed = 11;
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const pick = (a) => a[Math.floor(rnd() * a.length)];
const iso = (s) => new RealDate(Date.parse('2026-10-18T10:00:00.000Z') + s * 1000).toISOString();
const texts = ['Fix the parser', 'Готово, проверь', 'line one\nline two', 'x'.repeat(300), 'a "quoted" <tag> & more', ''];

const claudeLine = (i) => {
  const ts = iso(i);
  switch (Math.floor(rnd() * 9)) {
    case 0: return { type: 'user', timestamp: ts, uuid: `u${i}`, message: { role: 'user', content: pick(texts) || 'hi' } };
    case 1: return { type: 'assistant', timestamp: ts, uuid: `a${i}`, message: { id: `m${i}`, role: 'assistant', model: 'claude-x', content: [{ type: 'text', text: pick(texts) }], stop_reason: pick(['end_turn', null]) } };
    case 2: return { type: 'assistant', timestamp: ts, uuid: `a${i}`, message: { id: `m${i}`, role: 'assistant', content: [{ type: 'tool_use', id: `toolu_${i}`, name: pick(['Bash', 'Read', 'Edit']), input: { command: 'ls', file_path: '/srv/a.go', old_string: 'a', new_string: 'b' } }], stop_reason: 'tool_use' } };
    case 3: return { type: 'user', timestamp: ts, uuid: `r${i}`, message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: `toolu_${i - 1}`, content: pick(['ok', '12 files', '']) }] } };
    case 4: return { type: 'assistant', timestamp: ts, uuid: `t${i}`, message: { role: 'assistant', content: [{ type: 'thinking', thinking: 'hm' }, { type: 'text', text: 'Done' }], stop_reason: 'end_turn' } };
    case 5: return pick(['{broken', '', '[]', '{"type":"progress","data":{}}', '{"type":"system","subtype":"x"}']);
    case 6: return { type: 'user', timestamp: ts, isSidechain: true, uuid: `s${i}`, message: { role: 'user', content: 'side task' } };
    case 7: return { type: 'summary', summary: pick(['A title', 'Другой']) };
    default: return { type: 'user', timestamp: ts, uuid: `u${i}`, message: { role: 'user', content: [{ type: 'text', text: pick(texts) || 'yo' }] } };
  }
};

const codexLine = (i) => {
  const ts = iso(i);
  const row = (type, payload) => ({ timestamp: ts, type, payload });
  switch (Math.floor(rnd() * 9)) {
    case 0: return row('event_msg', { type: 'user_message', message: pick(texts) || 'hi' });
    case 1: return row('event_msg', { type: 'agent_message', message: pick(texts) || 'ok' });
    case 2: return row('response_item', { type: 'function_call', name: 'shell', arguments: JSON.stringify({ command: ['ls', '-la'] }), call_id: `call_${i}` });
    case 3: return row('response_item', { type: 'function_call_output', call_id: `call_${i - 1}`, output: JSON.stringify({ output: 'a.go\n', metadata: { exit_code: pick([0, 1]) } }) });
    case 4: return row('turn_context', { cwd: '/work/acme', model: pick(['gpt-5', 'model-x']), effort: 'medium' });
    case 5: return row('response_item', { type: 'reasoning', summary: [{ type: 'summary_text', text: 'thinking about it' }] });
    case 6: return pick(['{broken', '', '{"type":"event_msg","payload":{"type":"token_count","info":null}}']);
    case 7: return row('response_item', { type: 'message', role: pick(['user', 'assistant']), content: [{ type: pick(['input_text', 'output_text']), text: pick(texts) || 'x' }] });
    default: return row('event_msg', { type: 'task_complete', last_agent_message: 'done' });
  }
};

const transcript = (lineOf, n, head = []) => [...head, ...Array.from({ length: n }, (_, i) => lineOf(i + 1))]
  .map((r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n';
const codexHead = [{ timestamp: iso(0), type: 'session_meta', payload: { id: '55555555-5555-4555-8555-555555555555', cwd: '/work/acme', timestamp: iso(0), cli_version: '0.1.0' } }];

const { readEntriesBefore } = await import('@/lib/session-parser');
const { readCodexEntriesBefore } = await import('@/lib/session-parser-codex');

// --- generated page reads ----------------------------------------------------------------------------------------------
if (process.env.GEN_RANDOM) {
  const cases = [];
  const file = path.join(root, 'case.jsonl');
  for (let i = 0; i < Number(process.env.GEN_RANDOM); i++) {
    const codex = rnd() < 0.5;
    const big = rnd() < 0.05;
    let content = codex ? transcript(codexLine, Math.floor(rnd() * (big ? 6000 : 300)), rnd() < 0.8 ? codexHead : [])
      : transcript(claudeLine, Math.floor(rnd() * (big ? 6000 : 300)));
    if (rnd() < 0.2) content = content.replace(/\n/g, '\r\n');
    if (rnd() < 0.2) content = content.slice(0, Math.floor(rnd() * content.length));
    fs.writeFileSync(file, content);
    const size = Buffer.byteLength(content);
    const before = pick([size, size, Math.floor(rnd() * size), 0, 1, size - 1]);
    const limit = pick([256, 1, 2, 5, 10, 50, 1000]);
    const until = pick([undefined, undefined, size, Math.floor(rnd() * size), before]);
    const r = codex ? await readCodexEntriesBefore(file, before, limit, until) : await readEntriesBefore(file, before, limit);
    cases.push({ codex, content, before, limit, until: until ?? -1, answer: JSON.stringify({ entries: r.entries, startByteOffset: r.startByteOffset, hasMore: r.hasMore, replaceEntries: codex }) });
  }
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ now: FIXED, cases }));
  fs.rmSync(root, { recursive: true, force: true });
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  process.exit(0);
}

// --- the route ----------------------------------------------------------------------------------------------------------
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
if (OLD_HEADERS.length !== 2) throw new Error('OLD_RUNTIME_TOKEN_HEADERS (two names) is required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];
const TOKEN = 'c'.repeat(64);

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
fs.writeFileSync(path.join(dataDir(), 'cli-token'), TOKEN, { mode: 0o600 });

const CLAUDE = 'home/.claude/projects/-work-acme/11111111-1111-4111-8111-111111111111.jsonl';
const CODEX = 'home/.codex/sessions/2026/10/18/rollout-2026-10-18T10-00-00-55555555-5555-4555-8555-555555555555.jsonl';
const files = [
  { path: CLAUDE, content: transcript(claudeLine, 330) },
  { path: CODEX, content: transcript(codexLine, 330, codexHead) },
  { path: 'home/.claude/projects/-work-acme/crlf.jsonl', content: transcript(claudeLine, 40).replace(/\n/g, '\r\n') },
  { path: 'home/.claude/projects/-work-acme/cut.jsonl', content: transcript(claudeLine, 30).slice(0, -17) },
  { path: 'home/.claude/projects/-work-acme/empty.jsonl', content: '' },
  { path: 'home/elsewhere/s.jsonl', content: transcript(claudeLine, 3) },
];
for (const f of files) {
  const file = path.join(root, f.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, f.content);
}
const sizeOf = (p) => Buffer.byteLength(files.find((f) => f.path === p).content);

const dispatch = createApiDispatcher([{ route: '/api/timeline/entries', load: () => import('@/pages/api/timeline/entries') }]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const ses = { cookie: 'session-token=good' };
const tok = { 'x-cli-token': TOKEN };
const P = (p) => `$ROOT/${p}`;
const E = '/api/timeline/entries';
const cs = sizeOf(CLAUDE);
const xs = sizeOf(CODEX);
const q = (p, before, extra = []) => [['jsonlPath', P(p)], ['beforeByte', String(before)], ...extra];
const L3 = [['limit', '3']];
const CRLF = 'home/.claude/projects/-work-acme/crlf.jsonl';
const requests = [
  ['GET', E, q(CLAUDE, cs, L3), {}],
  ['GET', E, q(CLAUDE, cs, L3), tok],
  ['GET', E, q(CLAUDE, cs, L3), { 'x-cli-token': 'wrong', 'x-old-cli-token': TOKEN }],
  ['GET', E, q(CLAUDE, cs), ses],
  ['GET', E, q(CLAUDE, cs, L3), { ...ses, 'if-none-match': 'ETAG' }],
  ['POST', E, q(CLAUDE, cs), ses],
  ['DELETE', E, [], ses],
  ['GET', E, [], ses],
  ['GET', E, [['jsonlPath', ''], ['beforeByte', '5']], ses],
  ['GET', E, q('home/elsewhere/s.jsonl', 10), ses],
  ['GET', E, q('home/.claude/projects/../../elsewhere/s.jsonl', 10), ses],
  ['GET', E, [['jsonlPath', P('home/.claude/projects/-work-acme/x.json')], ['beforeByte', '5']], ses],
  ['GET', E, [['jsonlPath', P(CLAUDE)]], ses],
  ...['', 'abc', '-1', '-0', '0', ' 12', '+40', '0x10', '12abc', '1e3', '7.9'].map((b) => ['GET', E, [['jsonlPath', P(CLAUDE)], ['beforeByte', b]], ses]),
  ...['', '0', '-0', '1', '5', 'x', '3.7', '1e2', '99999999999999999999'].map((l) => ['GET', E, q(CRLF, sizeOf(CRLF), [['limit', l]]), ses]),
  ['GET', E, q(CLAUDE, Math.floor(cs / 2), [['limit', '40']]), ses],
  ['GET', E, q(CLAUDE, Math.floor(cs / 2), [['limit', '20']]), ses],
  ['GET', E, q(CLAUDE, 100, [['limit', '20']]), ses],
  ['GET', E, q(CLAUDE, cs - 1, [['limit', '3']]), ses],
  ['GET', E, q('home/.claude/projects/-work-acme/crlf.jsonl', sizeOf('home/.claude/projects/-work-acme/crlf.jsonl'), [['limit', '7']]), ses],
  ['GET', E, q('home/.claude/projects/-work-acme/cut.jsonl', sizeOf('home/.claude/projects/-work-acme/cut.jsonl')), ses],
  ['GET', E, q('home/.claude/projects/-work-acme/empty.jsonl', 10), ses],
  ['GET', E, q(CRLF, sizeOf(CRLF) + 10, [['limit', '3']]), ses],
  ['GET', E, q('home/.claude/projects/-work-acme/cut.jsonl', sizeOf('home/.claude/projects/-work-acme/cut.jsonl') + 300), ses],
  ['GET', E, q('home/.claude/projects/-work-acme/missing.jsonl', 10), ses],
  ['GET', E, q(CODEX, xs, [['limit', '30']]), ses],
  ['GET', E, q(CODEX, xs, [['limit', '10']]), ses],
  ['GET', E, q(CODEX, Math.floor(xs / 2), [['limit', '10'], ['untilByte', String(xs)]]), ses],
  ['GET', E, q(CODEX, Math.floor(xs / 2), [['limit', '10'], ['untilByte', String(Math.floor(xs / 3))]]), ses],
  ['GET', E, q(CODEX, Math.floor(xs / 2), [['limit', '10'], ['untilByte', String(Math.floor(xs / 2) + 500)]]), ses],
  ...['', '-1', '-0', 'x', '99999999999999999999'].map((u) => ['GET', E, q(CODEX, Math.floor(xs / 2), [['limit', '4'], ['untilByte', u]]), ses]),
  ['GET', E, q(CODEX, xs + 1000, [['limit', '5']]), ses],
  ['GET', E, q(CODEX, '99999999999999999999', [['limit', '5']]), ses],
  ['GET', E, q('home/.codex/sessions/missing.jsonl', 10), ses],
  ['GET', `/agents${E}/`, q(CLAUDE, cs, [['limit', '2']]), ses],
  // Left to the old runtime by the Go route.
  ['GET', E, q(CRLF, sizeOf(CRLF), [['limit', '-5']]), ses, true],
  ['GET', E, q(CRLF, sizeOf(CRLF) + 2_000_000), ses, true],
  ['GET', E, q(CRLF, sizeOf(CRLF), [['beforeByte', '10']]), ses, true],
  ['GET', E, [['jsonlPath', P(CLAUDE)], ['jsonlPath', P(CLAUDE)], ['beforeByte', '10']], ses, true],
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const scrub = (s) => s.split(root).join('$ROOT');
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
const out = [];
let lastEtag = '';
for (const [method, urlPath, query, headers, stays] of requests) {
  const h = toOld({ ...headers });
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v.replace('$ROOT', root))}`).join('&');
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + urlPath + (qs ? `?${qs}` : ''), { method, headers: h }, (r) => {
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
  out.push({ method, path: urlPath, query, headers, stays: !!stays, status: res.status, answerHeaders: kept, answer: scrub(res.text) });
}
server.close();
fs.writeFileSync(path.join(here, '..', 'entries.json'), JSON.stringify({ now: FIXED, token: TOKEN, files, requests: out }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests → ${path.join(here, '..', 'entries.json')}`);
