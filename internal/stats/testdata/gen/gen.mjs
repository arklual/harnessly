// Lays out Claude Code and Codex files in a home, sends requests to the previous runtime's own API dispatcher and its
// /api/stats/* handlers, and writes what each answered, and the cache files it left, to ../golden.json. The Go test lays
// out the same files, replays the same steps on the same clock and must answer byte for byte.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> \
//   node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// GEN_TZ picks the zone (default America/New_York: the 30-day window crosses a DST change). With GEN_RANDOM=<n>
// GEN_OUT=<file> it writes n generated scenarios instead (TS_GOLDEN of the Go test); GEN_SEED varies them.
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const TZ = process.env.GEN_TZ ?? 'America/New_York';
process.env.TZ = TZ;
let NOW = Date.parse(process.env.GEN_NOW ?? '2026-11-03T14:30:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(NOW); }
  static now() { return NOW; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'stats-golden-')));
const home = path.join(root, 'home');
const bin = path.join(root, 'bin');
fs.mkdirSync(home, { recursive: true });
fs.mkdirSync(bin, { recursive: true });
process.env.HOME = home;
process.env.GEN_SHELL_PATH = `${bin}:/usr/bin:/bin`;

const SCRIPTS = {
  claude: `#!/bin/sh
input=$(cat)
case "$input" in *FAILME*) echo "boom" >&2; exit 3;; esac
h=$(printf '%s' "$input" | sha1sum | cut -c1-12)
printf '[BRIEF]  \\nBusy day %s\\n## -- \\n[DETAIL]\\n## Project\\n### Area\\n- Task %s\\n' "$h" "$h"
`,
  codex: `#!/bin/sh
out=""; prev=""; last=""
for a in "$@"; do if [ "$prev" = "-o" ]; then out="$a"; fi; prev="$a"; last="$a"; done
h=$(printf '%s' "$last" | sha1sum | cut -c1-12)
printf '[BRIEF]\\nCodex day %s\\n[DETAIL]\\n- item %s\\n' "$h" "$h" > "$out"
echo "stdout noise"
`,
};
for (const [name, text] of Object.entries(SCRIPTS)) fs.writeFileSync(path.join(bin, name), text, { mode: 0o755 });

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const DATA = dataDir();

const ROUTES = ['aggregated', 'cache-status', 'facets', 'history', 'overview', 'projects', 'sessions', 'uptime', 'daily-report/list', 'daily-report/generate'];
const dispatch = createApiDispatcher(ROUTES.map((r) => ({ route: `/api/stats/${r}`, load: () => import(`@/pages/api/stats/${r}`) })));
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

// --- deterministic randomness -------------------------------------------------------------------------------------------
let seed = Number(process.env.GEN_SEED ?? 7);
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const int = (n) => Math.floor(rnd() * n);
const pick = (a) => a[int(a.length)];
const chance = (p) => rnd() < p;

// --- files --------------------------------------------------------------------------------------------------------------
const HOME_MARK = '@HOME@';
const resolvePath = (p) => p.replace(/^\$HOME/, home).replace(/^\$DATA/, DATA);
const writeFile = (f) => {
  const file = resolvePath(f.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  if (f.dir) {
    fs.mkdirSync(file, { recursive: true });
    return { path: f.path, dir: true };
  }
  const buf = Buffer.from(f.content.split(HOME_MARK).join(home), 'utf8');
  const raw = f.raw ? Buffer.concat([buf, Buffer.from(f.raw, 'base64')]) : buf;
  if (f.append) fs.appendFileSync(file, raw); else fs.writeFileSync(file, raw);
  const sec = Math.floor(f.mtime / 1000);
  fs.utimesSync(file, sec, sec);
  const st = fs.statSync(file, { bigint: true });
  return { path: f.path, b64: Buffer.from(f.content, 'utf8').toString('base64'), raw: f.raw, append: !!f.append, mtimeNs: String(st.mtimeNs) };
};

const iso = (ms) => new RealDate(ms).toISOString();
const offsetIso = (ms, h) => {
  const d = new RealDate(ms + h * 3600_000).toISOString().slice(0, 19);
  const sign = h >= 0 ? '+' : '-';
  return `${d}${sign}${String(Math.abs(h)).padStart(2, '0')}:00`;
};
const stamp = (now) => {
  const back = pick([5 * 60_000, 3600_000, 6 * 3600_000, 86400_000, 2 * 86400_000, 6 * 86400_000, 8 * 86400_000, 15 * 86400_000, 29 * 86400_000, 31 * 86400_000, 45 * 86400_000, 70 * 86400_000]);
  const ms = now - int(back) - 1000;
  switch (int(20)) {
    case 0: return 'bad';
    case 1: return '';
    case 2: return undefined;
    case 3: return iso(now + 2 * 3600_000);
    case 4: return offsetIso(ms, pick([3, -5, 9]));
    case 5: return iso(ms).slice(0, 10);
    default: return iso(ms);
  }
};
const sessions = ['s-aaaa', 's-bbbb', 's-cccc', '7', 's-dddd'];
const models = ['claude-opus-4-6', 'claude-sonnet-4-5-20250929', 'claude-haiku-4-5', '<synthetic>', 'claude-opus-5-5', 'claude-fable-5-1', '', 'claude-x-9'];
const cwds = [`${HOME_MARK}/work/app`, `${HOME_MARK}/Workspace/github.com/acme/tool`, `${HOME_MARK}/Documents/notes`, '/srv/other', `${HOME_MARK}`];
const texts = ['fix the build', '/review src', '/compact', '<command-message>run</command-message><command-name>/deploy</command-name>', '<local-command-caveat>x</local-command-caveat>',
  'Привет, сделай отчёт', 'emoji 😀 text', 'x'.repeat(420), '', '  ', 'FAILME please', '<task-notification>done</task-notification>'];
const count = () => pick([0, 1, 12, 345, 6789, 120000, 1.5, '12', 'abc', null, undefined, -4]);
const usage = () => {
  if (chance(0.05)) return pick([null, 'x', 0, true, []]);
  const u = { input_tokens: count(), output_tokens: count(), cache_read_input_tokens: count(), cache_creation_input_tokens: count() };
  if (chance(0.4)) u.cache_creation = pick([{ ephemeral_5m_input_tokens: count(), ephemeral_1h_input_tokens: count() }, { ephemeral_1h_input_tokens: count() }, null, 'x']);
  for (const k of Object.keys(u)) if (u[k] === undefined) delete u[k];
  return u;
};
const content = () => pick([
  'plain text',
  [{ type: 'text', text: pick(texts) }],
  [{ type: 'tool_result', content: 'ok' }],
  [{ type: 'text', text: pick(texts) }, { type: 'text', text: pick(texts) }],
  pick(texts),
  [],
  [null, 'x', { type: 'text' }],
]);
const claudeLine = (now, st) => {
  const e = {};
  const ts = stamp(now);
  if (ts !== undefined) e.timestamp = ts;
  if (chance(0.9)) e.sessionId = chance(0.95) ? st.sid : pick(['', null, 7]);
  if (chance(0.5)) e.cwd = st.cwd;
  if (chance(0.03)) e.cwd = pick(['', 5]);
  e.uuid = `u-${int(40)}`;
  switch (int(12)) {
    case 0: case 1: case 2: case 3:
      e.type = 'user';
      e.message = chance(0.95) ? { role: 'user', content: content() } : pick([null, 'str']);
      break;
    case 11:
      return JSON.stringify(pick([{ type: 'system', subtype: 'turn_duration', timestamp: e.timestamp }, { type: 'summary', summary: 'x' }, { type: 'file-history-snapshot' }]));
    default: {
      e.type = 'assistant';
      e.requestId = `req-${int(30)}`;
      if (chance(0.06)) { e.message = pick([null, 'str', 0, []]); break; }
      const id = chance(0.15) ? undefined : (chance(0.5) && st.lastId ? st.lastId : `msg-${int(60)}`);
      st.lastId = id;
      const m = { role: 'assistant', content: pick([[{ type: 'text', text: 'ok' }], [{ type: 'tool_use', name: 'Bash' }, { type: 'tool_use', name: 'Read' }], 'text', [{ type: 'thinking' }, { type: 'tool_use', name: 'Edit' }]]) };
      if (id !== undefined) m.id = chance(0.95) ? id : 42;
      if (chance(0.95)) m.model = pick(models);
      if (chance(0.95)) m.usage = usage();
      e.message = m;
    }
  }
  return JSON.stringify(e);
};
const oddLine = () => pick(['', '   ', '{"type":"user"', 'not json', 'null', '[1,2]', '"str"', '{"type":"user","timestamp":"2026-11-01T10:00:00.000Z","message":{"content":"big 1e400 ok"},"n":1e400}']);
const claudeFile = (now, lines) => {
  const st = { sid: pick(sessions), cwd: pick(cwds) };
  const out = [];
  for (let i = 0; i < lines; i++) {
    if (chance(0.08)) out.push(oddLine());
    else out.push(claudeLine(now, st));
    if (chance(0.05)) st.sid = pick(sessions);
  }
  const eol = chance(0.15) ? '\r\n' : '\n';
  let text = out.join(eol);
  if (chance(0.7)) text += eol;
  return text;
};
const localParts = (ms) => {
  const d = new RealDate(ms);
  return [String(d.getFullYear()), String(d.getMonth() + 1).padStart(2, '0'), String(d.getDate()).padStart(2, '0')];
};
const codexFile = (now, n) => {
  const start = now - int(pick([3600_000, 86400_000, 6 * 86400_000, 20 * 86400_000, 40 * 86400_000]));
  const lines = [];
  if (chance(0.9)) {
    const payload = { id: chance(0.9) ? `c-${n}` : pick([null, 7]), cwd: chance(0.8) ? pick(cwds) : undefined };
    if (chance(0.85)) payload.timestamp = pick([iso(start), 'bad', iso(start)]);
    lines.push({ timestamp: iso(start), type: 'session_meta', payload });
  }
  let t = start;
  let total = { input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_output_tokens: 0 };
  for (let i = 0, m = 3 + int(20); i < m; i++) {
    t += int(20 * 60_000);
    const ts = chance(0.93) ? iso(t) : pick(['bad', undefined, iso(now + 3600_000)]);
    switch (int(6)) {
      case 0: lines.push({ timestamp: ts, type: 'turn_context', payload: { model: pick(['gpt-5.4', 'gpt-5.5', 'gpt-5.4-mini', 'gpt-5.2-codex', 'o9', 7]), effort: pick(['high', 'low', undefined, 3]) } }); break;
      case 1: case 2: lines.push({ timestamp: ts, type: 'event_msg', payload: { type: 'user_message', message: pick(texts.concat(['# AGENTS.md x', '<env>', 5])) } }); break;
      case 3: {
        total = { input_tokens: total.input_tokens + int(9000), cached_input_tokens: total.cached_input_tokens + int(6000), output_tokens: total.output_tokens + int(800), reasoning_output_tokens: total.reasoning_output_tokens + int(100) };
        const info = chance(0.1) ? null : { total_token_usage: { ...total, total_tokens: total.input_tokens + total.output_tokens }, last_token_usage: { total_tokens: int(300000) }, model_context_window: pick([258400, 128000, undefined, 400000]) };
        const rl = chance(0.5) ? { primary: { used_percent: int(100), window_minutes: pick([300, undefined, null]), resets_at: Math.floor(now / 1000) + int(9000) }, secondary: pick([{ used_percent: 12.5, resets_in_seconds: 400 }, null, { used_percent: 'x' }]) } : undefined;
        lines.push({ timestamp: ts, type: 'event_msg', payload: { type: 'token_count', info, rate_limits: rl } });
        break;
      }
      case 4: lines.push({ timestamp: ts, type: 'response_item', payload: { type: pick(['function_call', 'custom_tool_call', 'message']), name: 'shell' } }); break;
      default: lines.push(oddLine());
    }
  }
  const [y, mo, d] = localParts(start);
  return { path: `$HOME/.codex/sessions/${y}/${mo}/${d}/rollout-${n}.jsonl`, content: lines.map((l) => (typeof l === 'string' ? l : JSON.stringify(l))).join('\n') + '\n', mtime: t };
};
const historyFile = (now) => {
  const lines = [];
  for (let i = 0, n = 5 + int(30); i < n; i++) {
    if (chance(0.06)) { lines.push(oddLine()); continue; }
    lines.push(JSON.stringify({ display: pick(texts.concat(['/model opus', '/cost', '/model x', ' /init '])), timestamp: pick([now - int(40 * 86400_000), now - int(3600_000), 0, 'bad', 1.5e20]), project: '/x', sessionId: 's' }));
  }
  return lines.join('\n') + '\n';
};
const facet = (i) => JSON.stringify({
  session_id: chance(0.8) ? `f-${i}` : undefined,
  outcome: pick(['achieved', 'partial', '', undefined]),
  session_type: pick(['coding', 'debug']),
  goal_categories: pick([{ bugfix: 2, feature: 1 }, { feature: '3', '7': 1 }, null, [4, 5], 'x']),
  user_satisfaction_counts: pick([{ happy: 1 }, undefined]),
  claude_helpfulness: pick(['very', undefined]),
  brief_summary: 'did things',
});

const scenarioFiles = (now, size) => {
  const files = [];
  const projects = ['-home-agent-work-app', '-srv-other', '-home-agent-tool', '7'];
  for (let i = 0, n = size; i < n; i++) {
    const proj = pick(projects);
    const name = chance(0.08) ? `agent-${i}.jsonl` : `${crypto.createHash('md5').update(String(i + seed)).digest('hex').slice(0, 8)}.jsonl`;
    files.push({ path: `$HOME/.claude/projects/${proj}/${name}`, content: claudeFile(now, 5 + int(40)), mtime: now - int(86400_000) });
  }
  if (chance(0.3)) files.push({ path: '$HOME/.claude/projects/-srv-other/sub.jsonl', dir: true });
  if (chance(0.3)) files.push({ path: `$HOME/.claude/projects/-srv-other/raw.jsonl`, content: '{"type":"user","timestamp":"' + iso(now - 3600_000) + '","sessionId":"s-raw","message":{"content":"bad ', raw: Buffer.from([0xff, 0xfe, 0x22, 0x7d, 0x7d, 0x0a]).toString('base64'), mtime: now - 7200_000 });
  for (let i = 0, n = Math.ceil(size / 3); i < n; i++) files.push(codexFile(now, i));
  if (chance(0.8)) files.push({ path: '$HOME/.claude/history.jsonl', content: historyFile(now), mtime: now - 60_000 });
  for (let i = 0, n = int(4); i < n; i++) files.push({ path: `$HOME/.claude/usage-data/facets/f${i}.json`, content: chance(0.9) ? facet(i) : '{bad', mtime: now - int(40 * 86400_000) });
  if (chance(0.2)) files.push({ path: '$HOME/.claude/usage-data/facets/dir.json', dir: true });
  if (chance(0.5)) {
    const day = (k) => new RealDate(now - k * 86400_000);
    const ymd = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    files.push({ path: `$DATA/stats/daily-reports/${ymd(day(1))}.json`, content: JSON.stringify({ date: ymd(day(1)), brief: 'old brief', detail: '- a', generatedAt: iso(now - 86400_000), locale: pick(['en', 'ru']), provider: pick(['claude', undefined, 'codex']), extra: 1 }, null, 2), mtime: now });
    files.push({ path: `$DATA/stats/daily-reports/bad.json`, content: '{"date":"', mtime: now });
  }
  if (chance(0.4)) {
    const days = {};
    for (let k = 1; k < 40; k += 1 + int(5)) {
      const d = new RealDate(now - k * 86400_000);
      days[`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`] = {
        messageCount: int(30), sessionCount: int(4), toolCallCount: int(50), hourCounts: { '3': 2, '14': int(9) },
        modelTokens: { 'claude-opus-4-6': { input: int(1e5), output: int(1e4), cacheRead: chance(0.1) ? null : int(1e6), cacheCreation: 300, cacheCreation5m: 200, cacheCreation1h: 100 } },
        sessions: [{ id: 'old-1', start: iso(d.getTime()), end: iso(d.getTime() + int(7200_000)), messages: 3 }],
      };
    }
    const lc = new RealDate(now - (1 + int(4)) * 86400_000);
    files.push({ path: '$DATA/stats/cache.json', content: JSON.stringify({ version: chance(0.9) ? 3 : 2, lastComputedDate: `${lc.getFullYear()}-${String(lc.getMonth() + 1).padStart(2, '0')}-${String(lc.getDate()).padStart(2, '0')}`, days, extra: 'kept' }), mtime: now });
  }
  if (chance(0.4)) {
    const days = {};
    for (const k of [1, 2, 3, 5, 35]) {
      const d = new RealDate(now - k * 86400_000);
      days[`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`] = { 's-old': [d.getTime(), d.getTime() + 60_000, d.getTime() + 20 * 60_000] };
    }
    files.push({ path: '$DATA/stats/uptime-cache.json', content: JSON.stringify({ version: 2, lastComputedDate: 'x', days }), mtime: now });
  }
  files.push({ path: '$DATA/config.json', content: JSON.stringify({ noteSummaryProvider: pick(['claude', 'codex', undefined]) }), mtime: now });
  return files;
};

// --- requests -----------------------------------------------------------------------------------------------------------
const WATCH = ['stats/cache.json', 'stats/uptime-cache.json'];
const snapshot = () => {
  const out = {};
  for (const w of WATCH) {
    const f = path.join(DATA, w);
    out[w] = fs.existsSync(f) ? fs.readFileSync(f, 'utf8') : null;
  }
  const dir = path.join(DATA, 'stats/daily-reports');
  if (fs.existsSync(dir)) for (const n of fs.readdirSync(dir)) out[`stats/daily-reports/${n}`] = fs.readFileSync(path.join(dir, n), 'utf8');
  return out;
};
const scrub = (s) => s.split(root).join('$ROOT');
let last = {};
const send = async (req) => {
  const qs = (req.query ?? []).map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
  const headers = { ...(req.headers ?? {}) };
  if (req.body !== undefined && !headers['content-type']) headers['content-type'] = 'application/json';
  const res = await new Promise((resolve, reject) => {
    const r = http.request(`${base}${req.path}${qs ? `?${qs}` : ''}`, { method: req.method, headers }, (resp) => {
      const chunks = [];
      resp.on('data', (c) => chunks.push(c));
      resp.on('end', () => resolve({ status: resp.statusCode, headers: resp.headers, body: Buffer.concat(chunks).toString('utf8') }));
    });
    r.on('error', reject);
    if (req.body !== undefined) r.write(req.body);
    r.end();
  });
  const answer = res.body;
  // The uptime writes its cache file without waiting for it: wait until the file stops changing.
  const settle = req.path.endsWith('/uptime') ? 3000 : 30;
  for (let waited = 0, prev = null; ; waited += 50) {
    await new Promise((r) => setTimeout(r, 50));
    const f = path.join(DATA, 'stats/uptime-cache.json');
    const cur = fs.existsSync(f) ? fs.readFileSync(f, 'utf8') : null;
    if (waited >= settle || (cur === prev && cur !== last['stats/uptime-cache.json'])) break;
    prev = cur;
  }
  const now = snapshot();
  const written = {};
  for (const k of new Set([...Object.keys(last), ...Object.keys(now)])) if (last[k] !== now[k]) written[k] = now[k] ?? null;
  last = now;
  const keep = {};
  for (const h of ['content-type', 'etag', 'allow']) if (res.headers[h] !== undefined) keep[h] = res.headers[h];
  return { kind: 'request', ...req, status: res.status, answerHeaders: keep, answer: scrub(answer), written };
};

const COOKIE = { cookie: 'session-token=good' };
const P = ['today', '7d', '30d', 'all'];
const randomRequest = (now) => {
  const route = pick(ROUTES.concat(['overview', 'sessions', 'uptime', 'projects', 'aggregated']));
  const query = [];
  if (['aggregated', 'facets', 'history', 'overview', 'projects', 'sessions'].includes(route) && chance(0.9)) query.push(['period', pick(P.concat(['bogus']))]);
  if (route === 'history' && chance(0.6)) query.push(['limit', pick(['3', '0', '-2', 'x', '25'])]);
  if (route === 'daily-report/list') {
    if (chance(0.6)) query.push(['offset', pick(['0', '1', '2.5', '-3', 'x'])]);
    if (chance(0.6)) query.push(['limit', pick(['1', '2', '60', '0', '1.5'])]);
  }
  if (route === 'daily-report/generate') {
    const d = new RealDate(now - int(10) * 86400_000);
    const date = chance(0.9) ? `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` : pick(['2026-1-1', '', 'x']);
    const body = { date };
    if (chance(0.4)) body.provider = pick(['claude', 'codex', 'gemini', null]);
    if (chance(0.5)) body.locale = pick(['en', 'ru', 'xx', ' ']);
    if (chance(0.5)) body.force = pick([true, false, 1]);
    return { method: 'POST', path: '/api/stats/daily-report/generate', headers: COOKIE, body: JSON.stringify(body) };
  }
  return { method: 'GET', path: `${chance(0.1) ? '/agents' : ''}/api/stats/${route}`, query, headers: COOKIE };
};

const resetHome = () => {
  fs.rmSync(home, { recursive: true, force: true });
  fs.mkdirSync(home, { recursive: true });
  fs.rmSync(path.join(DATA, 'stats'), { recursive: true, force: true });
  fs.mkdirSync(DATA, { recursive: true });
  last = snapshot();
};

const runScenario = async (size, requests, extraSteps = []) => {
  resetHome();
  const steps = [{ kind: 'clock', now: NOW }];
  const files = scenarioFiles(NOW, size).map(writeFile);
  last = snapshot();
  for (let i = 0; i < requests; i++) {
    if (chance(0.12)) {
      NOW += pick([10_000, 61_000, 301_000, 3600_000]);
      steps.push({ kind: 'clock', now: NOW });
    }
    if (chance(0.1)) {
      const changed = [];
      const proj = pick(['-srv-other', '-home-agent-work-app']);
      changed.push(writeFile({ path: `$HOME/.claude/projects/${proj}/added-${i}.jsonl`, content: claudeFile(NOW, 10), mtime: NOW }));
      const c = codexFile(NOW, 900 + i);
      changed.push(writeFile(c));
      steps.push({ kind: 'write', files: changed });
    }
    steps.push(await send(randomRequest(NOW)));
  }
  for (const s of extraSteps) steps.push(typeof s === 'function' ? await s() : s);
  NOW += 3600_000;
  return { files, steps };
};

const golden = { tz: TZ, home: HOME_MARK, scripts: SCRIPTS, scenarios: [] };
if (process.env.GEN_RANDOM) {
  const n = Number(process.env.GEN_RANDOM);
  for (let i = 0; i < n; i++) golden.scenarios.push(await runScenario(2 + int(10), 4 + int(10)));
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify(golden));
} else {
  // Fixed cases: every route and period, refusals, conditional requests, clock steps across the memo lifetimes.
  const fixed = async () => {
    resetHome();
    const steps = [{ kind: 'clock', now: NOW }];
    const files = scenarioFiles(NOW, 14).map(writeFile);
    last = snapshot();
    const all = [];
    for (const r of ['overview', 'projects', 'sessions', 'aggregated', 'facets', 'history']) for (const p of P) all.push({ method: 'GET', path: `/api/stats/${r}`, query: [['period', p]], headers: COOKIE });
    all.push({ method: 'GET', path: '/api/stats/uptime', headers: COOKIE });
    all.push({ method: 'GET', path: '/api/stats/cache-status', headers: COOKIE });
    all.push({ method: 'GET', path: '/api/stats/daily-report/list', query: [['limit', '50']], headers: COOKIE });
    for (const req of all) steps.push(await send(req));
    steps.push(await send({ method: 'GET', path: '/api/stats/overview', query: [['period', 'all']] }));
    steps.push(await send({ method: 'POST', path: '/api/stats/overview', headers: COOKIE }));
    const etagStep = await send({ method: 'GET', path: '/agents/api/stats/sessions', query: [['period', '7d']], headers: COOKIE });
    steps.push(etagStep);
    steps.push(await send({ method: 'GET', path: '/api/stats/sessions', query: [['period', '7d']], headers: { ...COOKIE, 'if-none-match': etagStep.answerHeaders.etag } }));
    steps.push(await send({ method: 'GET', path: '/api/stats/history', query: [['limit', '2'], ['limit', '5']], headers: COOKIE }));
    const day = (k) => { const d = new RealDate(NOW - k * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`; };
    for (const body of [
      { date: day(1) }, { date: day(1), locale: 'ru', provider: 'codex' }, { date: day(2), force: true, provider: 'claude' }, { date: '2001-01-01' },
      { date: 'x' }, { date: day(0), provider: 'gemini' }, { date: day(3), provider: null },
    ]) steps.push(await send({ method: 'POST', path: '/api/stats/daily-report/generate', headers: COOKIE, body: JSON.stringify(body) }));
    steps.push(await send({ method: 'POST', path: '/api/stats/daily-report/generate', headers: COOKIE, body: 'null' }));
    steps.push(await send({ method: 'POST', path: '/api/stats/daily-report/generate', headers: COOKIE, body: '{bad' }));
    steps.push(await send({ method: 'GET', path: '/api/stats/daily-report/generate', headers: COOKIE }));
    // The day of a prompt that makes the CLI fail.
    const failDay = day(0);
    steps.push({ kind: 'write', files: [writeFile({ path: '$HOME/.claude/projects/-srv-other/fail.jsonl', content: JSON.stringify({ type: 'user', timestamp: iso(NOW - 1000), sessionId: 's-fail', message: { content: 'FAILME now' } }) + '\n', mtime: NOW })] });
    steps.push(await send({ method: 'POST', path: '/api/stats/daily-report/generate', headers: COOKIE, body: JSON.stringify({ date: failDay, provider: 'claude', force: true }) }));
    for (const adv of [30_000, 31_000, 240_000, 86400_000]) {
      NOW += adv;
      steps.push({ kind: 'clock', now: NOW });
      steps.push({ kind: 'write', files: [writeFile({ path: '$HOME/.claude/projects/-srv-other/grow.jsonl', content: claudeFile(NOW, 6) + '\n', append: true, mtime: NOW })] });
      for (const r of ['overview', 'uptime', 'sessions', 'daily-report/list']) steps.push(await send({ method: 'GET', path: `/api/stats/${r}`, query: r === 'uptime' || r === 'daily-report/list' ? [] : [['period', '7d']], headers: COOKIE }));
    }
    NOW += 3600_000;
    return { files, steps };
  };
  golden.scenarios.push(await fixed());
  for (let i = 0; i < 5; i++) golden.scenarios.push(await runScenario(4 + int(8), 12));
  const out = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'golden.json');
  fs.writeFileSync(out, JSON.stringify(golden, null, 1) + '\n');
}
server.close();
fs.rmSync(root, { recursive: true, force: true });
