// Posts status lines to the previous runtime's own API dispatcher and its statusline handler, on fixture layouts,
// session stats and settings files, and writes what each answered, the files it left and what it broadcast to
// ../golden.json. The Go test lays out the same files, sends the same requests and must answer byte for byte. The
// person's statusLine commands run for real (sh).
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> OLD_RUNTIME_MODULE_PREFIX=<prefix of its module
//   names> OLD_RUNTIME_META_KEY=<its tab metadata key> node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it posts n generated status lines instead (TS_GOLDEN of the Go test).
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
const META = process.env.OLD_RUNTIME_META_KEY;
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
if (!M || !META || OLD_HEADERS.length !== 2) throw new Error('OLD_RUNTIME_MODULE_PREFIX, OLD_RUNTIME_META_KEY and OLD_RUNTIME_TOKEN_HEADERS (two names) are required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'statusline-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const here = path.dirname(new URL(import.meta.url).pathname);
globalThis.__golden = { broadcasts: [] };

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const { forgetUltracodeSessions } = await import(`@/lib/${M}ultracode`);
const DATA = dataDir();
const TOKEN = 'd'.repeat(64);
const SID = '3f0c9a52-6d1e-4b7a-9c2d-0e1f2a3b4c5d';
const SID_ULTRA = '7c6b5a49-3827-4615-a4b3-c2d1e0f9a8b7';
const SID_NEW = '11111111-2222-4333-8444-555555555555';
const tab = (id, sessionId, effort) => ({ id, sessionName: `hs-ws-a-pane-a-${id}`, name: id, order: 0, cwd: '/srv/app', panelType: 'claude-code',
  agentState: { providerId: 'claude', sessionId, jsonlPath: null, summary: null }, ...(effort ? { [META]: { launch: { model: 'opus', effort } } } : {}) });
const data = {
  'cli-token': TOKEN,
  'workspaces.json': JSON.stringify({ workspaces: [{ id: 'ws-a', name: 'A', directories: ['/srv/app'] }], groups: [] }, null, 2),
  'workspaces/ws-a/layout.json': JSON.stringify({ root: { type: 'pane', id: 'pane-a', tabs: [tab('tab-1', SID, 'high'), tab('tab-2', SID_ULTRA, 'ultracode'), tab('tab-3', null, 'ultracode')] } }, null, 2),
  [`session-stats/${SID}.json`]: JSON.stringify({ custom: 1, sessionId: SID, transcriptPath: '/srv/old.jsonl', model: 'Old', 7: 'seven' }),
  'rate-limits.json': JSON.stringify({ ts: 1792280000, codex: { ts: 1792280000, five_hour: { used_percentage: 3, resets_at: 1792290000, at: 1792280000, source: 'codex-usage' }, seven_day: null, source: 'codex-usage' } }),
};
for (const [name, content] of Object.entries(data)) {
  fs.mkdirSync(path.dirname(path.join(DATA, name)), { recursive: true });
  fs.writeFileSync(path.join(DATA, name), content, { mode: 0o600 });
}
const P = (p) => path.join(root, p);
const writeFile = (rel, content) => { fs.mkdirSync(path.dirname(P(rel)), { recursive: true }); fs.writeFileSync(P(rel), content); };

const dispatch = createApiDispatcher([{ route: '/api/status/statusline', load: () => import('@/pages/api/status/statusline') }]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
const scrub = (s) => s.split(root).join('$ROOT');
const unscrub = (s) => s.split('$ROOT').join(root);
const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const filesNow = () => {
  const out = {};
  const dir = path.join(DATA, 'session-stats');
  for (const name of fs.existsSync(dir) ? fs.readdirSync(dir).sort() : []) out[`session-stats/${name}`] = fs.readFileSync(path.join(dir, name), 'utf8');
  out['rate-limits.json'] = fs.readFileSync(path.join(DATA, 'rate-limits.json'), 'utf8');
  return out;
};
const send = async (method, url, headers, body) => {
  forgetUltracodeSessions();
  globalThis.__golden.broadcasts = [];
  const h = toOld({ ...headers });
  const payload = body === undefined ? undefined : Buffer.from(unscrub(body), 'utf8');
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: payload ? { ...h, 'content-length': payload.length } : h }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    // A body refused before it is read may meet a closed socket; the answer has come by then.
    req.on('error', (err) => { if (err.code !== 'EPIPE' && err.code !== 'ECONNRESET') reject(err); });
    req.end(payload);
  });
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  return { status: res.status, answerHeaders: kept, answer: scrub(res.text), broadcasts: globalThis.__golden.broadcasts.map(scrub) };
};

const tok = { 'x-cli-token': TOKEN, 'content-type': 'application/json' };
const ses = { cookie: 'session-token=good', 'content-type': 'application/json' };
const S = '/api/status/statusline';
const full = (over = {}) => JSON.stringify({
  session_id: SID, transcript_path: '/srv/app/.claude/s.jsonl', cwd: '/srv/app', model: { id: 'claude-opus-4-6', display_name: 'Opus 4.6' },
  workspace: { current_dir: '/srv/app', project_dir: '$ROOT/proj' }, version: '2.1.0', output_style: { name: 'default' },
  cost: { total_cost_usd: 1.2345, total_duration_ms: 1000, total_lines_added: 3 }, exceeds_200k_tokens: false,
  context_window: { total_input_tokens: 15000, total_output_tokens: 1200, context_window_size: 200000, used_percentage: 12.5, remaining_percentage: 87.5,
    current_usage: { input_tokens: 10, output_tokens: 5, cache_creation_input_tokens: 2000, cache_read_input_tokens: 9000 } },
  rate_limits: { five_hour: { used_percentage: 42, resets_at: 1792290000 }, seven_day: { used_percentage: 11.5, resets_at: 1792800000 } },
  effort: { level: 'high' }, ...over,
});

if (process.env.GEN_RANDOM) {
  let seed = 5;
  const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  writeFile('home/.claude/settings.json', JSON.stringify({ statusLine: { type: 'command', command: 'cat' } }));
  const sessions = [SID, SID_ULTRA, SID_NEW, 'not-a-uuid', '', null, 5, [SID_NEW], undefined];
  const val = () => pick([0, 7, 1.5, -2, '12', null, undefined, true, { a: 1 }, [1, 2], 'x', 100000]);
  const cases = [];
  for (let i = 0; i < Number(process.env.GEN_RANDOM); i++) {
    const input = {};
    const set = (k, v) => { if (v !== undefined) input[k] = v; };
    set('session_id', pick(sessions));
    set('transcript_path', pick(['/srv/t.jsonl', null, undefined, 3]));
    set('model', pick([{ display_name: pick(['Opus', '', null, 4]) }, {}, null, 'flat', undefined]));
    set('effort', pick([{ level: pick(['high', 'xhigh', 'low', 5, null]) }, null, 'xhigh', undefined]));
    set('cost', pick([{ total_cost_usd: val() }, null, undefined]));
    set('exceeds_200k_tokens', pick([true, false, null, undefined, 'yes']));
    set('context_window', pick([undefined, null, 'flat', {
      total_input_tokens: val(), total_output_tokens: val(), context_window_size: val(), used_percentage: val(),
      current_usage: pick([undefined, null, 0, { input_tokens: val(), cache_creation_input_tokens: val(), cache_read_input_tokens: val() }]),
    }]));
    set('rate_limits', pick([undefined, null, {}, { five_hour: pick([null, undefined, { used_percentage: 50, resets_at: 1792290000 + Math.floor(rnd() * 9) * 600 }]),
      seven_day: pick([null, undefined, { used_percentage: 20, resets_at: 1792800000 }, 0]) }]));
    set('workspace', pick([undefined, { project_dir: '/nonexistent/proj' }, { current_dir: '/srv' }]));
    const order = Object.entries(input).sort(() => rnd() - 0.5);
    const body = JSON.stringify(Object.fromEntries(order));
    const before = filesNow();
    const r = await send('POST', S, tok, body);
    const after = filesNow();
    const changed = {};
    for (const [k, v] of Object.entries(after)) if (before[k] !== v) changed[k] = v;
    cases.push({ body, ...r, changed });
  }
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ now: FIXED, token: TOKEN, data: Object.fromEntries(Object.entries(data).map(([k, v]) => [k, v.split(`"${META}"`).join('"meta"')])), cases }));
  server.close();
  fs.rmSync(root, { recursive: true, force: true });
  console.log(`${cases.length} generated cases → ${process.env.GEN_OUT}`);
  process.exit(0);
}

const steps = [
  { req: ['POST', S, { 'content-type': 'application/json' }, '{}'] },
  { req: ['POST', S, ses, '{}'] },
  { req: ['GET', S, ses] },
  { req: ['GET', S, tok] },
  { req: ['POST', S, { ...ses, 'x-cli-token': 'wrong', 'x-old-cli-token': TOKEN }, '{}'] },
  { req: ['POST', S, tok, '{"session_id":'] },
  { req: ['POST', S, tok, JSON.stringify({ pad: 'a'.repeat(1 << 20) })] },
  { req: ['POST', S, tok, ''] },
  { write: 'home/.claude/settings.json', content: JSON.stringify({ model: 'opus', statusLine: { type: 'command', command: 'cat' } }) },
  { req: ['POST', S, tok, ''] },
  { req: ['POST', S, { ...tok, 'content-type': 'text/plain' }, 'hello "there"'] },
  { req: ['POST', S, { ...tok, 'content-type': 'application/x-www-form-urlencoded' }, 'a=1&b=x%20y&a=3&c'] },
  { req: ['POST', S, { 'x-old-cli-token': TOKEN, 'content-type': 'application/json; charset=utf-8' }, 'null'] },
  { req: ['POST', S, tok, full()] },
  { req: ['POST', S, tok, full({ transcript_path: undefined, model: { display_name: 'Opus 4.6 (1M)' }, cost: undefined, rate_limits: { five_hour: null, seven_day: { used_percentage: 12, resets_at: 1792800000 } } })] },
  { req: ['POST', S, tok, full({ session_id: SID_ULTRA, effort: { level: 'xhigh' }, rate_limits: { five_hour: null, seven_day: null } })] },
  { req: ['POST', S, tok, full({ session_id: SID_NEW, effort: { level: 'xhigh' }, rate_limits: undefined, transcript_path: null })] },
  { req: ['POST', S, tok, full({ session_id: 'not-a-uuid', exceeds_200k_tokens: true, context_window: { current_usage: { input_tokens: '5', cache_read_input_tokens: 2 } } })] },
  { req: ['POST', S, tok, full({ session_id: 42, effort: { level: 7 }, context_window: 'flat', model: 'flat' })] },
  { req: ['POST', S, tok, JSON.stringify({ b: 1, a: { 2: 'two', 1: 'one', z: 1.0, big: 1e21, small: 1e-7 }, '10': 'ten', s: 'tab\there ' })] },
  { write: 'proj/.claude/settings.local.json', content: JSON.stringify({ statusLine: { command: "printf 'from local'" } }) },
  { write: 'proj/.claude/settings.json', content: JSON.stringify({ statusLine: { command: 'echo from project; echo oops >&2; exit 3' } }) },
  { req: ['POST', S, tok, full()] },
  { remove: 'proj/.claude/settings.local.json' },
  { req: ['POST', S, tok, full()] },
  { write: 'proj/.claude/settings.json', content: JSON.stringify({ statusLine: { command: '   ' } }) },
  { req: ['POST', S, tok, full()] },
  { write: 'proj/.claude/settings.json', content: '{"statusLine":{"command":' },
  { req: ['POST', S, tok, full()] },
  { write: 'proj/.claude/settings.json', content: JSON.stringify({ statusLine: { command: 5 } }) },
  { req: ['POST', S, tok, full({ workspace: { project_dir: 7 } })] },
  { req: ['POST', S, tok, full({ workspace: { project_dir: '' } })] },
  { write: 'home/.claude/settings.json', content: JSON.stringify({ statusLine: { command: "printf 'caf\\303\\251 \\377 end'" } }) },
  { req: ['POST', '/agents' + S + '/', tok, full({ session_id: undefined })] },
  { write: 'home/.claude/settings.json', content: JSON.stringify({ statusLine: { command: 'echo only-err >&2' } }) },
  { req: ['POST', S, tok, '{}'] },
  { write: 'home/.claude/settings.json', content: String.fromCharCode(0xfeff) + JSON.stringify({ statusLine: { command: 'cat' } }) },
  { req: ['POST', S, tok, '{}'] },
  { remove: 'home/.claude/settings.json' },
  { req: ['POST', S, tok, String.fromCharCode(0xfeff) + '{"session_id":null}'] },
];

const out = [];
let seen = filesNow();
for (const step of steps) {
  if (!step.req) {
    if (step.write) writeFile(step.write, step.content);
    if (step.remove) fs.rmSync(P(step.remove));
    out.push(step);
    continue;
  }
  const [method, url, headers, body] = step.req;
  const r = await send(method, url, headers, body);
  const now = filesNow();
  const changed = {};
  for (const [k, v] of Object.entries(now)) if (seen[k] !== v) changed[k] = scrub(v);
  seen = now;
  out.push({ method, path: url, headers, body: body !== undefined && body.length > 100000 ? { repeat: body.length } : body ?? null, ...r, changed });
}
server.close();
const golden = {
  now: FIXED, token: TOKEN,
  data: Object.fromEntries(Object.entries(data).map(([k, v]) => [k, v.split(`"${META}"`).join('"meta"')])),
  steps: out,
};
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify(golden, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} steps → ${path.join(here, '..', 'golden.json')}`);
