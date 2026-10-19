// Sends requests to the previous runtime's own API dispatcher and its conversation-sources and message-counts handlers
// on Claude and Codex transcripts that grow, shrink and are replaced between requests, and writes what each answered to
// ../golden.json. The Go test replays the same steps and must answer byte for byte.
//
// Nothing of the previous runtime's own naming is spelled here: its header names, platform prefix and the hosts and
// project keys its link rules know come from the caller, and the golden file uses neutral stand-ins (OLD_RUNTIME_SUBST
// maps each stand-in to the real word; transcripts are written with the real words and answers read back with the
// stand-ins).
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_SUBST='<stand-in>=<real>;…' \
//   node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated transcripts, each read in random stretches, with what both
// routes answered after each stretch instead (SOURCES_RANDOM of the Go test).
import crypto from 'node:crypto';
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

const SUBST = (process.env.OLD_RUNTIME_SUBST ?? '').split(';').filter(Boolean).map((p) => p.split('='));
if (!SUBST.length) throw new Error('OLD_RUNTIME_SUBST is required');
// Byte offsets are in the answers: a stand-in must be as long as the word it stands for.
for (const [a, b] of SUBST) if (Buffer.byteLength(a) !== Buffer.byteLength(b)) throw new Error(`stand-in ${a} is not as long as its word`);
const toReal = (s) => SUBST.reduce((t, [a, b]) => t.split(a).join(b), s);
const toNeutral = (s) => SUBST.reduce((t, [a, b]) => t.split(b).join(a), s);
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
if (!OLD_API) throw new Error('OLD_RUNTIME_PLATFORM_API is required');

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'sources-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const here = path.dirname(new URL(import.meta.url).pathname);

let seed = 7;
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const pick = (a) => a[Math.floor(rnd() * a.length)];
const int = (n) => Math.floor(rnd() * n);
const iso = (s) => new RealDate(Date.parse('2026-10-18T10:00:00.000Z') + s * 1000).toISOString();

// Stand-ins: a.example.io (documents, boards), ai.example.io (the platform's reader), tracker.example.io and
// youtrack.example.io (issues), gitlab.example.io (GitLab), project keys ABC, EXP and OPS.
const linkTexts = [
  'See [the report](https://a.example.io/a/report-1) and https://a.example.io/b/board-2?k=abc.',
  'Board docs: https://a.example.io/b/api and https://a.example.io/b/api/ and https://a.example.io/c/x',
  'MR https://gitlab.example.io/group/sub/proj/-/merge_requests/42/diffs, also https://gitlab.com/a/b/-/merge_requests/007!',
  'Issues ABC-123, EXP-7 and OPS-1; not ABC-1234567, x/ABC-5, ABC-12-3, ABC-9x, a.ABC-4 or ABCX-2.',
  'Tracker https://tracker.example.io/issue/OPS-9 and https://youtrack.example.io/youtrack/issue/ABC-77/ plus https://tracker.example.io/agiles/1',
  'Reader https://ai.example.io/agents/docs/my-doc and https://ai.example.io/agents/docs/a/b',
  '[Artifact](https://claude.ai/public/artifacts/abc-123) https://claude.ai/code/artifact/x_y/ https://github.com/acme/x/pull/3',
  'Local http://localhost:3000/x http://127.1/x http://0.0.0.0:8080 https://[::1]:3000/ and https://www.a.example.io/a/x',
  '[label with ] bracket](https://x.example.org/a) [](https://x.example.org/empty) [a\nb](https://x.example.org/nl)',
  `[${'L'.repeat(201)}](https://x.example.org/long) [${'L'.repeat(200)}](https://x.example.org/edge) [${'😀'.repeat(100)}](https://x.example.org/emoji) [${'😀'.repeat(101)}](https://x.example.org/emoji2)`,
  'Trailing https://example.com/page). and «https://example.org/ru» and “https://example.org/q”’ and https://example.org/x?!;:',
  'Same doc twice: [Report](https://a.example.io/a/report-1#part) https://a.example.io/a/report-1/ https://ai.example.io/agents/docs/report-1',
  'Spaces [x](https://x.example.org/a b) and (https://x.example.org/paren) and `https://x.example.org/tick` <https://x.example.org/angle>',
  'Unicode https://пример.рф/путь https://example.org/%D0%BF and https://EXAMPLE.org/Case and https://user:pw@example.org:443/a',
  'Bad http:// https://) https://exa mple.org http://example.org:99999/ x https://a..b/ yhttps://glued.example.org/z',
  'Готово: [отчёт](https://a.example.io/a/otchet), задача ABC-42 и EXP-300000.',
  'No links here at all.',
  '',
];
const commands = [
  'cd /work/acme && ls /srv/data/file.txt',
  "cat '/opt/x y' \"/etc/hosts\" x=/var/log/a.log (/usr/bin/env)",
  'cat /proc/cpuinfo /sys/class/x /dev/null',
  'tail /tmp/claude-1000/-work-acme/abc/tasks/123.output /tmp/other/file',
  'echo $HOME/x ./rel /a/b/ /a/b/c.d@e+f-g',
  'grep -r foo /work/acme/src',
  'ls',
];
const files = ['/work/acme/src/a.go', '/work/acme/README.md', '/srv/data/file.txt', '/work/acme/src/b.go'];

const claudeLine = (i) => {
  const ts = iso(i);
  switch (int(10)) {
    case 0: case 1: case 2:
      return { type: 'assistant', timestamp: ts, uuid: `a${i}`, requestId: `req${int(40)}`, message: { id: `m${i}`, role: 'assistant', model: 'claude-x', content: [{ type: 'text', text: pick(linkTexts) }], stop_reason: 'end_turn' } };
    case 3: {
      const name = pick(['Bash', 'Read', 'Edit', 'Write', 'MultiEdit', 'Grep', 5, 'constructor', 'toString', '__proto__', '10', '2']);
      const input = name === 'Bash' ? { command: pick(commands), description: 'run' } : { file_path: pick(files), old_string: 'a', new_string: 'b', content: 'x', pattern: 'p' };
      return { type: 'assistant', timestamp: ts, uuid: `t${i}`, requestId: pick([`req${int(40)}`, null, undefined, 7]), message: { id: `m${i}`, role: 'assistant', content: [{ type: 'tool_use', id: `toolu_${i}`, name, input }], stop_reason: 'tool_use' } };
    }
    case 4: return { type: 'user', timestamp: ts, uuid: `r${i}`, message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: `toolu_${i - 1}`, content: 'ok' }] } };
    case 5: return { type: 'user', timestamp: ts, uuid: `u${i}`, message: { role: 'user', content: pick(['Fix it', 'see https://x.example.org/user-link', '', [{ type: 'text', text: 'hi' }], [[1]], 7]) } };
    case 6: return pick(['{broken', '', '[]', '"str"', '42', '{"type":"progress","data":{}}', '{"type":"system","subtype":"x"}', '{"type":"assistant","isMeta":true,"message":{"content":[{"type":"tool_use","name":"Meta"}]}}',
      '{"typ\\u0065":"assistant","requestId":"esc","message":{"content":[{"type":"tool_use","name":"Esc\\u00e9\\"q"}]}}', '{"type":"assistant","type":"user","message":{"content":"dup"}}',
      ' \t{"type":"user","message":{"content":"lead space"}} ', '{"type":"user","message":{"content":"x"}}garbage', '{"type":"assistant","requestId":1.0,"message":{}}',
      '{"type":"assistant","requestId":"1","message":{}}', '{"type":"assistant","requestId":-0,"uuid":"z"}', '{"type":"assistant","requestId":1e400,"message":{"content":[{"type":"tool_use","name":["a",["b"]]}]}}',
      '{"type":"assistant","requestId":{"a":1},"message":{"content":[{"type":"tool_use","name":{"x":1}},{"type":"tool_use","name":0},{"type":"tool_use","name":true}]}}',
      '{"type":"assistant","requestId":"","uuid":"fallback-not-taken"}', '{"type":"assistant","requestId":null,"uuid":"fallback"}', '{"type":"user","message":{"content":"bad \\x escape"}}',
      '{"type":"user","message":{"content":"ctl \u0001 char"}}', '{"type":"user","message":{"content":[]}}', '{"type":"user","message":{"content":[null,1,"s"]}}', '{"type":"user","message":"str"}',
      '{"type":"user","isSidechain":"true","message":{"content":"not boolean"}}', '﻿{"type":"user","message":{"content":"bom"}}',
      '{"type":"assistant","requestId":01,"message":{}}', '{"type":"assistant","requestId":1.,"message":{}}', '{"a":[1,2,{"b":"}"}],"type":"user","message":{"content":"nested ] } \\" quote"}}']);
    case 7: return { type: 'assistant', timestamp: ts, isSidechain: true, uuid: `s${i}`, message: { role: 'assistant', content: [{ type: 'tool_use', name: 'Side' }, { type: 'text', text: 'https://x.example.org/side' }] } };
    case 8: return { type: 'summary', summary: 'A title' };
    default: return { type: 'assistant', timestamp: ts, uuid: `a${i}`, message: { id: `m${i}`, role: 'assistant', content: [{ type: 'thinking', thinking: 'hm' }, { type: 'text', text: pick(linkTexts) }], stop_reason: 'end_turn' } };
  }
};

const codexLine = (i) => {
  const ts = iso(i);
  const row = (type, payload) => ({ timestamp: ts, type, payload });
  switch (int(14)) {
    case 0: return row('event_msg', { type: 'user_message', message: pick(['hi', '# AGENTS.md instructions\nx', '<environment_context>x</environment_context>', '  ', 'see ABC-5']) });
    case 1: return row('event_msg', { type: 'agent_message', message: pick(linkTexts) || 'ok', ...(rnd() < 0.3 ? { phase: pick(['final', 'commentary']) } : {}) });
    case 2: return row('response_item', { type: 'function_call', name: pick(['shell', 'exec_command', 'write_stdin', '', 'apply_patch']), arguments: JSON.stringify({ command: ['bash', '-lc', pick(commands)], workdir: '/work/acme', cmd: pick(commands) }), call_id: `call_${i}` });
    case 3: return row('response_item', { type: 'function_call_output', call_id: `call_${i - 1}`, output: JSON.stringify({ output: 'a.go\n', metadata: { exit_code: 0 } }) });
    case 4: return row('turn_context', { cwd: '/work/acme', model: 'model-x' });
    case 5: return row('event_msg', { type: 'exec_command_begin', call_id: `exec_${i}`, command: ['bash', '-lc', pick(commands)], cwd: pick(['/work/acme', '/srv/data', '']) });
    case 6: return row('response_item', { type: 'custom_tool_call', status: 'completed', call_id: `call_patch_${i}`, name: 'apply_patch', input: `*** Begin Patch\n*** Add File: ${pick(files)}\n+x\n*** Update File: /work/acme/src/b.go\n@@\n-a\n+b\n*** End Patch` });
    case 7: return row('response_item', { type: 'message', role: pick(['user', 'assistant']), content: [{ type: pick(['input_text', 'output_text']), text: pick(linkTexts) || 'x' }], ...(rnd() < 0.3 ? { phase: 'final' } : {}) });
    case 8: return row('event_msg', { type: pick(['task_started', 'task_complete', 'turn_aborted']) });
    case 9: return row('response_item', { type: pick(['web_search_call', 'local_shell_call']), action: { type: 'search', query: 'x' } });
    case 10: return pick(['{broken', '', '{"type":"event_msg","payload":{"type":"token_count","info":null}}', '{"type":"response_item","payload":"str"}', '{"type":"event_msg","payload":{"type":["task_started"]}}']);
    default: {
      // The same message in both mirrors, as Codex writes it.
      const text = pick(['mirror one', 'mirror two https://x.example.org/m']);
      return [row('event_msg', { type: 'agent_message', message: text }), row('response_item', { type: 'message', role: 'assistant', content: [{ type: 'output_text', text }] })];
    }
  }
};

const lines = (lineOf, n, head = []) => [...head, ...Array.from({ length: n }, (_, i) => lineOf(i + 1)).flat()]
  .map((r) => (typeof r === 'string' ? r : JSON.stringify(r)));
const text = (ls) => ls.join('\n') + '\n';
const codexHead = [{ timestamp: iso(0), type: 'session_meta', payload: { id: '55555555-5555-4555-8555-555555555555', cwd: '/work/acme', timestamp: iso(0), cli_version: '0.1.0' } }];

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
if (OLD_HEADERS.length !== 2) throw new Error('OLD_RUNTIME_TOKEN_HEADERS (two names) is required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];
const TOKEN = 'c'.repeat(64);
fs.writeFileSync(path.join(dataDir(), 'cli-token'), TOKEN, { mode: 0o600 });

const dispatch = createApiDispatcher([
  { route: `${OLD_API}/sources`, load: () => import(`@/pages${OLD_API}/sources`) },
  { route: '/api/timeline/message-counts', load: () => import('@/pages/api/timeline/message-counts') },
]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;
const API = '/api/platform';

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const scrub = (s) => toNeutral(s.split(root).join('$ROOT'));
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
let lastEtag = '';
let lastKeptEtag = '';
const send = async (method, urlPath, query, headers) => {
  const h = toOld({ ...headers });
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v.replace('$ROOT', root))}`).join('&');
  const real = urlPath.replace(API, OLD_API);
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + real + (qs ? `?${qs}` : ''), { method, headers: h }, (r) => {
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
  // The ETag is of the bytes sent: with stand-ins in place of the words, it is that of the stand-in text.
  const neutral = toNeutral(res.text);
  if (kept.etag && neutral !== res.text) kept.etag = `"${crypto.createHash('sha1').update(neutral).digest('base64url').slice(0, 27)}"`;
  if (kept.etag && res.status === 304) kept.etag = lastKeptEtag;
  if (kept.etag) lastKeptEtag = kept.etag;
  return { status: res.status, answerHeaders: kept, answer: scrub(res.text) };
};

// A file op writes the stand-in text as the real words; "repeat" builds a large file from one line on both sides.
// "huge" is one assistant line of that many letters, longer than a stretch.
const hugeLine = (n) => `{"type":"assistant","message":{"content":[{"type":"text","text":"${'h'.repeat(n)} https://x.example.org/huge"}]}}`;
const contentOf = (op) => {
  if (op.repeat) return `${op.repeat.line}\n`.repeat(op.repeat.count) + (op.repeat.tail ?? '');
  if (op.huge) return `${hugeLine(op.huge)}\n${op.tail}`;
  if (op.corpus) return fs.readFileSync(path.join(here, '..', '..', '..', 'transcript', 'testdata', op.corpus), 'utf8');
  return op.content;
};
const apply = (op) => {
  const file = path.join(root, op.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const body = op.op === 'remove' ? '' : toReal(contentOf(op));
  switch (op.op) {
    case 'write': fs.writeFileSync(file, body); break;
    case 'append': fs.appendFileSync(file, body); break;
    case 'replace': fs.writeFileSync(`${file}.new`, body); fs.renameSync(`${file}.new`, file); break;
    case 'remove': fs.rmSync(file, { force: true }); break;
  }
};

// --- generated cases -----------------------------------------------------------------------------------------------------
if (process.env.GEN_RANDOM) {
  const cases = [];
  const CL = 'home/.claude/projects/-work-acme/r.jsonl';
  const CX = 'home/.codex/sessions/2026/10/18/rollout-r.jsonl';
  const ses = { cookie: 'session-token=good' };
  for (let i = 0; i < Number(process.env.GEN_RANDOM); i++) {
    const codex = rnd() < 0.5;
    const p = codex ? CX : CL;
    let all = text(codex ? lines(codexLine, int(rnd() < 0.05 ? 3000 : 200), rnd() < 0.8 ? codexHead : []) : lines(claudeLine, int(rnd() < 0.05 ? 3000 : 200)));
    if (rnd() < 0.03) all = all.replace(/\n([^\n]*)\n/, '\nnull\n$1\n');
    if (rnd() < 0.15) all = all.replace(/\n/g, '\r\n');
    if (rnd() < 0.05) all = all.replace(/\n/g, '\r');
    const cuts = Array.from({ length: int(4) }, () => int(all.length + 1)).sort((a, b) => a - b);
    const steps = [];
    let at = 0;
    apply({ op: 'replace', path: p, content: '' });
    for (const cut of [...cuts, all.length]) {
      // Cut where the stand-ins were already replaced, so no word is split between two appends.
      const piece = all.slice(at, cut);
      fs.appendFileSync(path.join(root, p), toReal(all).slice(at, cut));
      at = cut;
      steps.push({ append: piece });
      const cwd = pick([undefined, '/work/acme', '/srv/new']);
      const s = await send('GET', `${API}/sources`, [['jsonlPath', `$ROOT/${p}`], ...(cwd ? [['cwd', cwd]] : [])], ses);
      const m = await send('GET', '/api/timeline/message-counts', [['jsonlPath', `$ROOT/${p}`]], ses);
      steps.push({ cwd: cwd ?? '', sources: [s.status, s.answer], counts: [m.status, m.answer] });
    }
    cases.push({ path: p, steps });
  }
  server.close();
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ cases }));
  fs.rmSync(root, { recursive: true, force: true });
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  process.exit(0);
}

// --- fixed steps -----------------------------------------------------------------------------------------------------------
const CL = 'home/.claude/projects/-work-acme/11111111-1111-4111-8111-111111111111.jsonl';
const CX = 'home/.codex/sessions/2026/10/18/rollout-2026-10-18T10-00-00-55555555-5555-4555-8555-555555555555.jsonl';
const CRLF = 'home/.claude/projects/-work-acme/crlf.jsonl';
const NULLS = 'home/.claude/projects/-work-acme/nulls.jsonl';
const BIG = 'home/.claude/projects/-work-acme/big.jsonl';
const HUGE = 'home/.claude/projects/-work-acme/huge-line.jsonl';
const claudeAll = lines(claudeLine, 400);
const codexAll = lines(codexLine, 400, codexHead);
const half = (ls) => Math.floor(ls.length / 2);
const ses = { cookie: 'session-token=good' };
const tok = { 'x-cli-token': TOKEN };
const S = `${API}/sources`;
const M = '/api/timeline/message-counts';
const P = (p) => `$ROOT/${p}`;
const bigLine = JSON.stringify({ type: 'assistant', timestamp: iso(5), uuid: 'big', message: { id: 'mb', role: 'assistant', content: [{ type: 'text', text: `see https://x.example.org/big ABC-1 ${'z'.repeat(2000)}` }] } });

const steps = [
  { op: 'write', path: CL, content: text(claudeAll.slice(0, half(claudeAll))) },
  { op: 'write', path: CX, content: text(codexAll.slice(0, half(codexAll))) },
  { op: 'write', path: CRLF, content: text(lines(claudeLine, 60)).replace(/\n/g, '\r\n') },
  { op: 'write', path: NULLS, content: text([...lines(claudeLine, 10), 'null', ...lines(claudeLine, 5)]) },
  { op: 'write', path: 'home/.claude/projects/-work-acme/empty.jsonl', content: '' },
  { op: 'write', path: 'home/.claude/projects/-work-acme/corpus-a.jsonl', corpus: 'claude/recorded-2.1.284-teammate-messages.jsonl' },
  { op: 'write', path: 'home/.codex/sessions/2026/03/01/basic.jsonl', corpus: 'codex/corpus/basic.jsonl' },
  { op: 'write', path: 'home/.codex/sessions/2026/03/01/big.jsonl', corpus: 'codex/corpus/big.jsonl' },
  { op: 'write', path: 'home/elsewhere/s.jsonl', content: text(lines(claudeLine, 3)) },
  { op: 'write', path: 'home/.claude/projects/-work-acme/dir.jsonl/x', content: '' },
  { op: 'write', path: 'home/.claude/projects/-work-acme/cr.jsonl', content: text(lines(claudeLine, 30)).replace(/\n/g, '\r') },
  // Auth, methods and parameters.
  ['GET', S, [['jsonlPath', P(CL)]], {}],
  ['GET', S, [['jsonlPath', P(CL)]], tok],
  ['GET', S, [['jsonlPath', P(CL)]], { 'x-cli-token': 'wrong', 'x-old-cli-token': TOKEN }],
  ['POST', S, [['jsonlPath', P(CL)]], ses],
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', S, [['jsonlPath', P(CL)]], { ...ses, 'if-none-match': 'ETAG' }],
  ['GET', S, [['jsonlPath', P(CL)], ['cwd', '/work/acme']], ses],
  ['GET', S, [['jsonlPath', P(CL)], ['cwd', '/brand/new']], ses],
  ['GET', S, [['jsonlPath', P(CL)], ['cwd', 'relative']], ses],
  ['GET', S, [['jsonlPath', P(CL)], ['cwd', '/a'], ['cwd', '/b']], ses],
  ['GET', S, [], ses],
  ['GET', S, [['jsonlPath', '']], ses],
  ['GET', S, [['jsonlPath', P(CL)], ['jsonlPath', P(CL)]], ses],
  ['GET', S, [['jsonlPath', P('home/elsewhere/s.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/../../elsewhere/s.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/-work-acme/missing.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/-work-acme/dir.jsonl')]], ses],
  ['GET', S, [['jsonlPath', `${P(CL)}\u0000`]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/-work-acme/empty.jsonl')]], ses],
  ['GET', `/agents${S}/`, [['jsonlPath', P(CL)]], ses],
  ['GET', S, [['jsonlPath', P(CX)], ['cwd', '/work/acme']], ses],
  ['GET', S, [['jsonlPath', P(CRLF)]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/-work-acme/cr.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.claude/projects/-work-acme/corpus-a.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.codex/sessions/2026/03/01/basic.jsonl')]], ses],
  ['GET', S, [['jsonlPath', P('home/.codex/sessions/2026/03/01/big.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], {}],
  ['GET', M, [['jsonlPath', P(CL)]], tok],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], { ...ses, 'if-none-match': 'ETAG' }],
  ['POST', M, [['jsonlPath', P(CL)]], ses],
  ['HEAD', M, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [], ses],
  ['GET', M, [['jsonlPath', '']], ses],
  ['GET', M, [['jsonlPath', P(CL)], ['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P('home/elsewhere/s.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P('home/.claude/projects/-work-acme/missing.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P('home/.claude/projects/-work-acme/dir.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P(NULLS)]], ses],
  ['GET', M, [['jsonlPath', P(CX)]], ses],
  ['GET', M, [['jsonlPath', P(CRLF)]], ses],
  ['GET', M, [['jsonlPath', P('home/.claude/projects/-work-acme/cr.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P('home/.claude/projects/-work-acme/corpus-a.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P('home/.codex/sessions/2026/03/01/basic.jsonl')]], ses],
  ['GET', M, [['jsonlPath', P('home/.codex/sessions/2026/03/01/big.jsonl')]], ses],
  ['GET', `/agents${M}/`, [['jsonlPath', P(CL)]], ses],
  // The transcripts grow: a line still being written, then finished; then the rest.
  { op: 'append', path: CL, content: `${claudeAll[half(claudeAll)]}\n${claudeAll[half(claudeAll) + 1].slice(0, 40)}` },
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  { op: 'append', path: CL, content: `${claudeAll[half(claudeAll) + 1].slice(40)}\n` },
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  { op: 'append', path: CL, content: text(claudeAll.slice(half(claudeAll) + 2)) },
  ['GET', S, [['jsonlPath', P(CL)], ['cwd', '/work/acme']], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  { op: 'append', path: CX, content: text(codexAll.slice(half(codexAll))) },
  ['GET', S, [['jsonlPath', P(CX)]], ses],
  ['GET', M, [['jsonlPath', P(CX)]], ses],
  // Shrunk, then replaced by another file under the same name.
  { op: 'write', path: CL, content: text(claudeAll.slice(0, 20)) },
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  { op: 'replace', path: CL, content: text(lines(claudeLine, 30)) },
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  { op: 'remove', path: CL },
  ['GET', S, [['jsonlPath', P(CL)]], ses],
  ['GET', M, [['jsonlPath', P(CL)]], ses],
  // Past one 8 MiB stretch, and a single line longer than a stretch.
  { op: 'write', path: BIG, repeat: { line: bigLine, count: 4300, tail: `${claudeAll[3]}\n` } },
  ['GET', S, [['jsonlPath', P(BIG)]], ses],
  ['GET', M, [['jsonlPath', P(BIG)]], ses],
  { op: 'write', path: HUGE, huge: 9 * 1024 * 1024, tail: `${bigLine}\n` },
  ['GET', S, [['jsonlPath', P(HUGE)]], ses],
  ['GET', M, [['jsonlPath', P(HUGE)]], ses],
];

const out = [];
for (const step of steps) {
  if (!Array.isArray(step)) {
    apply(step);
    out.push(step);
    continue;
  }
  const [method, urlPath, query, headers] = step;
  out.push({ method, path: urlPath, query, headers, ...(await send(method, urlPath, query, headers)) });
}
server.close();
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify({ token: TOKEN, steps: out }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} steps → ${path.join(here, '..', 'golden.json')}`);
