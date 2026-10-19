// Runs the previous runtime's own code on generated inputs and writes what it answered to ../golden.json: the window
// forecasts, provider limits, the limits history reader, the spend history built from transcripts, the machine load
// monitor on a /proc of its own, the pure classifiers of the load, and its API routes (machines, health, push) through
// its dispatcher. The Go tests run the same inputs and must answer byte for byte.
//
// The previous runtime spells the product in module names, environment variables, a header and a few answers; those
// words come from the environment and are replaced with neutral ones in golden.json (an ETag is then recomputed over
// the replaced body).
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_MODULE_PREFIX=<its module word> \
//   OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> OLD_RUNTIME_APP=<its health app name> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// GOLDEN_SCALE=<n> multiplies the generated cases and GOLDEN_OUT=<file> writes elsewhere (a large set, not committed).
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const P = process.env.OLD_RUNTIME_MODULE_PREFIX;
const OLD_HEADER = process.env.OLD_RUNTIME_TOKEN_HEADER;
const OLD_APP = process.env.OLD_RUNTIME_APP;
if (!P || !OLD_HEADER || !OLD_APP) throw new Error('OLD_RUNTIME_MODULE_PREFIX, OLD_RUNTIME_TOKEN_HEADER and OLD_RUNTIME_APP are required');
const UP = P.toUpperCase();
const SCALE = Number(process.env.GOLDEN_SCALE ?? 1);
const NEUTRAL = { prefix: 'acme', app: 'harnessly', tokenHeader: 'x-cli-token', api: '/api/platform' };

let seed = 20261018;
const rnd = () => {
  seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
const int = (a, b) => a + Math.floor(rnd() * (b - a + 1));

const FIXED = Date.parse('2026-10-18T09:30:00.000Z');
let NOW = FIXED;
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(NOW); }
  static now() { return NOW; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'machines-golden-')));
const home = path.join(root, 'home');
const DATA = path.join(root, 'data');
const APP = path.join(root, 'app');
for (const d of [home, DATA, APP]) fs.mkdirSync(d, { recursive: true });
process.env.HOME = home;
process.env[`${UP}_AI_DIR`] = DATA;
process.env[`${UP}_APP_DIR`] = APP;
process.env[`${UP}_CODEXBAR_BIN`] = '/usr/local/bin/codexbar';
fs.writeFileSync(path.join(APP, `.${P}-release.json`), JSON.stringify({ version: '1.42.0', commit: 'abc1234' }));

const scrubText = (s) => s.split(root).join('$ROOT').split(`/api/${P}/`).join(`${NEUTRAL.api}/`).split(P).join(NEUTRAL.prefix)
  .split(OLD_APP).join(NEUTRAL.app);
const etagOf = (payload) => `"${crypto.createHash('sha1').update(payload).digest('base64url').slice(0, 27)}"`;

const forecastMod = await import(`@/lib/${P}-limits-forecast`);
const activityMod = await import(`@/lib/${P}-limits-activity`);
const historyMod = await import(`@/lib/${P}-limits-history`);
const machinesMod = await import(`@/lib/${P}-machines`);
const factsMod = await import(`@/lib/${P}-usage/facts`);
const reportMod = await import(`@/lib/${P}-usage/report`);
const loadMod = await import(`@/lib/${P}-machine-load`);
const scanMod = await import(`@/lib/${P}-machine-load-scan`);
const { WINDOW_SPECS, forecastWindow } = forecastMod;
const { buildActivity } = activityMod;

const round4 = (x) => Math.round(x * 1e4) / 1e4;

// --- spend events and samples -----------------------------------------------------------------------------------

const genEvents = (asOf) => {
  const events = [];
  const days = pick([0.3, 0.8, 1.5, 2, 4, 9, 15, 25, 41]);
  const busy = rnd() * 3 + 0.2;
  // Coarser steps over a long history keep the cases small; the slots stay ten minutes.
  const step = Math.max(600, Math.round((days * 86400) / 350 / 600) * 600);
  for (let t = asOf - days * 86400; t < asOf; t += step) {
    const hourMsk = Math.floor(((t + 3 * 3600) % 86400) / 3600);
    const workHour = hourMsk >= 9 && hourMsk <= 22;
    if (rnd() < (workHour ? 0.45 : 0.05)) events.push([Math.round(t + rnd() * 599), round4((rnd() * busy * step) / 600)]);
  }
  if (rnd() < 0.1) events.push([asOf - 100, 0], [asOf + 50, 1], [asOf - 44 * 86400, 2]);
  return events;
};

const genSamples = (spec, now) => {
  const mode = rnd();
  const reset = mode < 0.08 ? now - int(1, 3000) : Math.round(now + spec.period * (0.05 + rnd() * 0.95));
  const start = reset - spec.period;
  const out = [];
  // An earlier window, for calibration.
  if (rnd() < 0.6) {
    const prevReset = reset - spec.period + int(-60, 60);
    let u = 0;
    for (let t = prevReset - spec.period + int(0, 3600); t < prevReset && out.length < 300; t += int(60, spec.period / 40)) {
      u = Math.min(100, u + Math.max(0, Math.round((rnd() - 0.3) * 4)));
      out.push({ t, u, r: prevReset });
    }
  }
  const rate = rnd() * (spec.period <= 86400 ? 40 : 3);
  let u = rnd() < 0.2 ? int(0, 30) : 0;
  const from = Math.max(start, now - spec.lookback * (1 + rnd() * 3));
  const step = Math.max(20, Math.round(spec.fresh * pick([0.05, 0.1, 0.2, 0.3, 0.45])));
  const end = now - (rnd() < 0.08 ? spec.fresh * 1.5 : rnd() * spec.fresh * 0.3);
  for (let t = from + int(0, 300); t <= end; t += step + int(0, step)) {
    u += (rate * step) / 3600 + (rnd() - 0.5) * 2;
    if (rnd() < 0.03) u -= int(2, 20);
    const shown = mode > 0.95 ? 100 : Math.max(0, Math.min(100, rnd() < 0.8 ? Math.round(u) : u));
    out.push({ t, u: shown, r: reset + (rnd() < 0.1 ? int(-120, 120) : 0) });
    if (out.length > 400) break;
  }
  out.sort((a, b) => a.t - b.t);
  // One sample per moment, as the history reader gives them.
  return out.filter((s, i) => i === 0 || out[i - 1].t !== s.t);
};

// --- forecasts --------------------------------------------------------------------------------------------------

const forecasts = [];
for (let i = 0; i < 90 * SCALE; i += 1) {
  const kind = pick(['five_hour', 'seven_day']);
  const spec = WINDOW_SPECS[kind];
  const now = FIXED / 1000 + int(0, 30 * 86400) + rnd();
  const samples = genSamples(spec, now);
  const withActivity = rnd() < 0.75;
  const asOf = Math.floor(now - (rnd() < 0.7 ? int(0, 600) : int(900, 7200)));
  const events = withActivity ? genEvents(asOf) : null;
  const activity = events ? buildActivity(events, asOf) : null;
  forecasts.push({ kind, now, samples: samples.map((s) => [s.t, s.u, s.r]), asOf: events ? asOf : null, events,
    result: JSON.stringify(forecastWindow(samples, now, spec, activity)) });
}

// --- provider limits ---------------------------------------------------------------------------------------------

const historyDir = path.join(root, 'history');
fs.mkdirSync(historyDir);
const genWindow = (now, period) => {
  const w = { used_percentage: int(0, 100), resets_at: Math.round(now + (rnd() - 0.2) * period) };
  if (rnd() < 0.6) w.at = Math.round(now - int(0, 900));
  if (rnd() < 0.5) w.source = pick(['statusline', 'codexbar', 'codex-usage', 'codex-jsonl']);
  if (rnd() < 0.05) w.at = pick(['x', null, true]);
  return w;
};
const genRaw = (now) => {
  if (rnd() < 0.08) return pick([null, 0, '', 'x', 5, [], [1], true, {}]);
  const p = {};
  if (rnd() < 0.9) p.ts = rnd() < 0.95 ? now - int(0, 3600) + rnd() : pick(['x', null]);
  if (rnd() < 0.9) p.five_hour = rnd() < 0.95 ? genWindow(now, 5 * 3600) : pick([null, 'x', { used_percentage: '5', resets_at: 1 }]);
  if (rnd() < 0.9) p.seven_day = genWindow(now, 7 * 86400);
  if (rnd() < 0.4) p.plan = rnd() < 0.8 ? { name: pick(['Max 20x', 'Pro', 'Plus']) } : pick(['x', null, { name: 5 }]);
  if (rnd() < 0.3) p.codexbarError = rnd() < 0.8 ? { message: 'timeout' } : pick([null, { message: 1 }]);
  if (rnd() < 0.4) {
    p.pace = rnd() < 0.8
      ? { five_hour: { deltaPercent: int(-50, 50), willLastToReset: rnd() < 0.5, etaSeconds: pick([null, int(60, 9000)]) }, seven_day: rnd() < 0.5 ? null : { b: 1, a: 2 } }
      : pick(['x', null, []]);
  }
  if (rnd() < 0.5) {
    p.extra = [];
    for (let k = 0; k < int(0, 3); k += 1) {
      p.extra.push(rnd() < 0.85 ? { id: pick(['opus', 'sonnet', 'gpt-5.5', 'bad id!']), title: pick(['Opus', 'Sonnet']), window: genWindow(now, 7 * 86400) }
        : pick([{ id: 1, title: 'x' }, { id: 'x' }, { id: 'y', title: 'Y', window: null }]));
    }
  }
  return p;
};
const genHistoryLines = (now, raws) => {
  const lines = [];
  for (let t = now - 2 * 86400; t < now; t += pick([300, 1800, 3600, 7200])) {
    for (const p of ['claude', 'codex']) {
      if (rnd() < 0.5) continue;
      const w = {};
      const raw = raws[p];
      const fh = raw && typeof raw === 'object' && raw.five_hour && typeof raw.five_hour === 'object' ? raw.five_hour.resets_at : now + 3600;
      const sd = raw && typeof raw === 'object' && raw.seven_day && typeof raw.seven_day === 'object' ? raw.seven_day.resets_at : now + 86400;
      if (rnd() < 0.9) w.five_hour = [rnd() < 0.2 ? int(0, 1000) / 10 : int(0, 100), fh, rnd() < 0.1 ? t + 0.25 : Math.round(t)];
      if (rnd() < 0.9) w.seven_day = [int(0, 100), sd, Math.round(t)];
      if (rnd() < 0.3) w.opus = [int(0, 100), sd, Math.round(t)];
      lines.push(JSON.stringify({ t: Math.round(t), p, w }));
    }
  }
  lines.unshift(JSON.stringify({ t: Math.round(now - 9 * 86400), p: 'claude', w: { five_hour: [1, now - 8 * 86400, now - 9 * 86400] } }));
  lines.push('not json', '{"t":"x","p":"claude","w":{}}', '{"t":1,"p":"other","w":{}}', `{"t":${Math.round(now)},"p":"claude","w":[1]}`,
    `{"t":${Math.round(now)},"p":"claude","w":{"five_hour":[1,"x",3]}}`);
  return lines;
};

const limitsCases = [];
for (let i = 0; i < 40 * SCALE; i += 1) {
  const now = FIXED / 1000 + int(0, 20 * 86400) + rnd();
  const raws = { claude: genRaw(now), codex: genRaw(now) };
  const lines = genHistoryLines(now, raws);
  const file = path.join(historyDir, `h${i}.jsonl`);
  fs.writeFileSync(file, lines.join('\n') + '\n');
  const records = await historyMod.readHistory(file, now);
  const samples = (provider, id) => historyMod.samplesOf(records, provider, id);
  const withActivity = rnd() < 0.5;
  const asOf = Math.floor(now - int(0, 600));
  const events = withActivity ? genEvents(asOf) : null;
  const activity = events ? buildActivity(events, asOf) : null;
  const result = {};
  for (const p of ['claude', 'codex']) result[p] = JSON.stringify(machinesMod.buildProviderLimits(raws[p], p, samples, now, p === 'claude' ? activity : null));
  limitsCases.push({ now, raw: JSON.stringify(raws), history: lines, asOf: events ? asOf : null, events, result });
}

// --- the history reader, appended to and replaced between reads --------------------------------------------------

const historySteps = [];
{
  const file = path.join(historyDir, 'live.jsonl');
  let now = FIXED / 1000;
  const line = (t, u) => JSON.stringify({ t, p: 'claude', w: { five_hour: [u, now + 3600, t] } });
  const ops = [
    ['write', [line(now - 9 * 86400, 1), line(now - 100, 2)].join('\n') + '\n'],
    ['append', line(now - 50, 3) + '\n' + '{"t":' + Math.round(now) + ',"p":"claude","w":{"five_hour":[4,'],
    ['append', `${now + 3600},${now - 10}]}}\n`],
    ['append', ''],
    ['replace', line(now - 5, 9) + '\n'],
    ['truncate', ''],
    ['append', line(now - 1, 7) + '\n' + line(now - 1, 8) + '\n'],
  ];
  for (const [op, text] of ops) {
    if (op === 'write' || op === 'truncate') fs.writeFileSync(file, text);
    else if (op === 'append') fs.appendFileSync(file, text);
    else { fs.writeFileSync(`${file}.new`, text); fs.renameSync(`${file}.new`, file); }
    now += 30;
    const records = await historyMod.readHistory(file, now);
    historySteps.push({ op, text, now, samples: JSON.stringify(historyMod.samplesOf(records, 'claude', 'five_hour')) });
  }
}

// --- spend history from transcripts -----------------------------------------------------------------------------

const transcripts = [];
{
  const base = path.join(root, 'transcripts');
  const claudeRoot = path.join(base, 'claude', 'projects');
  const codexRoot = path.join(base, 'codex', 'sessions');
  const asOf = Math.floor(FIXED / 1000);
  const iso = (t) => new RealDate(t * 1000).toISOString();
  const claudeModels = ['claude-opus-4-6', 'claude-sonnet-4-5-20250929', 'claude-haiku-4-5-20251001', 'claude-opus-5-5[1m]', 'claude-fable-5', 'claude-mystery-opus', 'claude-x', '<synthetic>'];
  const codexModels = ['gpt-5.5', 'gpt-5.4-mini', 'gpt-5-codex-2025-09-15', 'openai/gpt-5.1', 'unknown-model', 'gpt-4o'];
  const files = [];
  const writeFile = (file, lines) => { fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, lines.join('\n') + '\n'); files.push(file); };
  const shared = [];
  for (let f = 0; f < 6 * SCALE; f += 1) {
    const lines = [];
    for (let k = 0; k < int(20, 120); k += 1) {
      const t = asOf - int(0, 46 * 86400);
      const req = `req_${f}_${k}`;
      const id = `msg_${f}_${k}`;
      const usage = { input_tokens: int(0, 5000), output_tokens: int(0, 3000), cache_read_input_tokens: int(0, 90000) };
      if (rnd() < 0.5) usage.cache_creation_input_tokens = int(0, 20000);
      if (rnd() < 0.5) usage.cache_creation = { ephemeral_5m_input_tokens: int(0, 9000), ephemeral_1h_input_tokens: rnd() < 0.5 ? 0 : int(0, 9000) };
      const entry = { type: 'assistant', requestId: req, uuid: `u${f}${k}`, timestamp: iso(t), message: { id, model: pick(claudeModels), usage } };
      if (rnd() < 0.05) entry.isApiErrorMessage = true;
      if (rnd() < 0.05) delete entry.message.id;
      if (rnd() < 0.03) delete entry.requestId;
      lines.push(JSON.stringify(entry));
      if (rnd() < 0.3) lines.push(JSON.stringify({ ...entry, timestamp: iso(t + 2), message: { ...entry.message, usage: { ...usage, output_tokens: usage.output_tokens + int(0, 900) } } }));
      if (rnd() < 0.1) shared.push(JSON.stringify(entry));
      if (rnd() < 0.1) lines.push(JSON.stringify({ type: 'user', message: { role: 'user', content: 'hello assistant' }, timestamp: iso(t) }));
      if (rnd() < 0.05) lines.push(JSON.stringify({ type: 'assistant', timestamp: iso(t), message: { id: `p${k}`, model: 'claude-opus-4-6', usage, content: [{ type: 'tool_use', input: { type: 'progress' } }] } }));
      if (rnd() < 0.05) lines.push(JSON.stringify({ type: 'progress', data: { type: 'assistant' } }));
      if (rnd() < 0.03) lines.push('{"type":"assistant", broken', '"assistant"', '[1,2]');
      if (rnd() < 0.03) lines.push(JSON.stringify({ type: 'assistant', timestamp: iso(t), message: { id: `n${k}`, usage } }));
      if (rnd() < 0.03) lines.push(JSON.stringify({ type: 'assistant', timestamp: 'yesterday', requestId: `bad${k}`, message: { id: `b${k}`, model: 'claude-opus-4-6', usage } }));
    }
    writeFile(path.join(claudeRoot, `-home-agent-p${f % 3}`, `s${f}.jsonl`), lines);
    if (f % 2 === 0) writeFile(path.join(claudeRoot, `-home-agent-p${f % 3}`, `s${f}`, 'subagents', `agent-${f}.jsonl`), lines.slice(0, 10));
  }
  writeFile(path.join(claudeRoot, '-home-agent-p0', 'resumed.jsonl'), shared);
  writeFile(path.join(claudeRoot, 'a', 'b', 'c', 'd', 'too-deep.jsonl'), shared.slice(0, 3));
  for (let f = 0; f < 3 * SCALE; f += 1) {
    const lines = [JSON.stringify({ type: 'session_meta', payload: { id: `c${f}`, cwd: '/home/agent/p1' } })];
    let input = 0; let output = 0; let cached = 0; let write = 0;
    let t = asOf - int(0, 44 * 86400);
    for (let k = 0; k < int(10, 80); k += 1) {
      t += int(5, 4000);
      if (rnd() < 0.2) lines.push(JSON.stringify({ timestamp: iso(t), type: 'turn_context', payload: { model: pick(codexModels), cwd: '/home/agent/p2' } }));
      const grow = rnd() < 0.8;
      if (grow) { input += int(0, 20000); output += int(0, 3000); cached += int(0, 15000); write += rnd() < 0.3 ? int(0, 900) : 0; }
      const info = rnd() < 0.05 ? null : { total_token_usage: { input_tokens: input, cached_input_tokens: cached, cache_write_input_tokens: write, output_tokens: output }, last_token_usage: {} };
      lines.push(JSON.stringify({ timestamp: rnd() < 0.03 ? 42 : iso(t), type: 'event_msg', payload: { type: 'token_count', info } }));
    }
    writeFile(path.join(codexRoot, '2026', '10', `${String(f).padStart(2, '0')}`, `rollout-${f}.jsonl`), lines);
  }
  files.sort();
  const facts = [];
  for (const file of files) {
    if (file.startsWith(claudeRoot) && path.relative(claudeRoot, file).split(path.sep).length > 4) continue;
    facts.push(file.startsWith(claudeRoot) ? await factsMod.parseClaudeFile(file) : await factsMod.parseCodexFile(file));
  }
  const acts = reportMod.buildActivities(facts, asOf);
  const digest = (a) => a && { start: a.start, firstAt: a.firstAt, asOf: a.asOf, slots: a.cum.length,
    cum: crypto.createHash('sha1').update(JSON.stringify(Array.from(a.cum))).digest('hex'), total: a.cum[a.cum.length - 1] };
  const tree = {};
  for (const file of files) tree[path.relative(base, file)] = fs.readFileSync(file, 'utf8');
  transcripts.push({ asOf, files: tree, claude: digest(acts.claude), codex: digest(acts.codex) });
}

// --- pure classifiers of the load -------------------------------------------------------------------------------

const words = ['--token', '--api-key', '--apikey', '-p', '-psecret', '--password', 'PASSWORD=', 'AUTH=', 'author=', 'auth=', '--author', '--oauth',
  'pat=', 'my_pat=', 'path=', 'patent=', 'x-pat-y=', 'DATABASE_URL=', 'sentry_dsn=', 'Authorization:', 'authorization=', 'Bearer', 'bearer',
  'basic', 'https://user:pw@host/x', 'postgres://u:secret@db:5432/d', '"quoted"', "'q'", 'key', 'monkey=1', '--key', '---token', '-token',
  'a-token=1', 'node', 'claude', '/usr/bin/codex', 'npx', '@anthropic-ai/claude-code', '@openai/codex', 'cli.js', 'x/claude/cli.js',
  '/home/agent/.local/share/claude/versions/2.1.3', '=', '==', ' ', '\t', ' ', 'value', 'v@l', 'a/b', 'тест', '🙂'];
const genCommand = () => {
  const parts = [];
  for (let k = 0; k < int(0, 9); k += 1) parts.push(pick(words) + (rnd() < 0.4 ? pick(['', 'abc', '=xyz', ' xyz', '-', '_x', 'Q']) : ''));
  return parts.join(pick([' ', ' ', '  ', '']));
};
const redact = [];
for (let i = 0; i < 600 * SCALE; i += 1) {
  const args = genCommand();
  redact.push([args, loadMod.redactCommand(args)]);
}
redact.push(['x'.repeat(170) + ' --token abc', loadMod.redactCommand('x'.repeat(170) + ' --token abc')]);
const agentCases = [];
for (let i = 0; i < 300 * SCALE; i += 1) {
  const comm = pick(['node', 'claude', 'codex', 'bash', 'python3', 'npx', 'tmux: server']);
  const args = genCommand();
  agentCases.push([comm, args, loadMod.isAgentProcess(comm, args)]);
}
const cgroups = ['0::/system.slice/acme-ai-agent.service', '0::/system.slice/runtime.service/tmux-spawn-1.scope', '0::/user.slice/user-1000.slice/session-3.scope',
  '1:name=systemd:/system.slice/x.service\n0::/user.slice', '12:cpu,name=systemd:/system.slice/a.slice/b.service/c', '0::/system.slice/', 'garbage', '',
  '0::/system.slice/nested.slice/deep.service', '2:memory:/system.slice/m.service'];
const units = cgroups.map((c) => [c, loadMod.systemServiceUnit(c)]);
const services = [];
for (let i = 0; i < 200 * SCALE; i += 1) {
  const rel = `/opt/${P}-ai`;
  const input = { cgroup: pick([...cgroups, null]), exe: pick([null, '/usr/bin/node', `${rel}/bin/x`, rel, `${rel}x`]), args: pick(['', `${rel}/node `, `${rel} `]) + genCommand() + pick(['', ` ${rel}/server.js`, ` -x ${rel}/a`]),
    cwd: pick([null, '/home/agent', `${rel}/current`]), runtimeUnit: pick([null, '/system.slice/runtime.service', '/system.slice/acme-ai-agent.service']) };
  services.push([input, loadMod.isSystemService({ ...input, cwd: input.cwd, exe: input.exe })]);
}

// --- the load monitor on a /proc of its own ---------------------------------------------------------------------

const UID = 1000;
// The previous runtime's release root, which it spells with the product word.
const REL = `/opt/${P}-ai`;
const loadScenario = { trees: {}, scans: [] };
{
  const procLink = path.join(root, 'proc');
  const trees = {};
  const proc = (pid, { comm, ppid, uid = UID, euid = uid, ticks = 0, start = 1000 + pid, rss = 1000, args = comm, cwd = '/home/agent', exe = '/usr/bin/x', cgroup = '0::/system.slice/runtime.service', noRss = false }) => ({
    [`${pid}/stat`]: `${pid} (${comm}) S ${ppid} 1 1 0 -1 4194560 100 0 0 0 ${Math.floor(ticks / 2)} ${Math.ceil(ticks / 2)} 0 0 20 0 1 0 ${start} 100000 50 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n`,
    [`${pid}/status`]: `Name:\t${comm}\nUid:\t${uid}\t${euid}\t${euid}\t${euid}\n${noRss ? '' : `VmRSS:\t   ${rss} kB\n`}`,
    [`${pid}/cmdline`]: args.split(' ').join('\0') + '\0',
    [`${pid}/cgroup`]: cgroup,
    [`${pid}/owner`]: String(uid),
    [`${pid}/cwd`]: { link: cwd },
    [`${pid}/exe`]: { link: exe },
  });
  const machine = (cpuTotal, idle, swapIn, mem, load) => ({
    stat: `cpu  ${cpuTotal - idle} 0 0 ${idle} 0 0 0 0 0 0\ncpu0 1 2 3 4\n`,
    vmstat: `nr_free_pages 1\npswpin ${swapIn}\npswpout 3\n`,
    meminfo: `MemTotal:       16000000 kB\nMemFree:          100000 kB\nMemAvailable:   ${mem} kB\nSwapTotal:       2000000 kB\nSwapFree:        1500000 kB\n`,
    loadavg: `${load} 1.00 0.50 2/300 4000\n`,
  });
  const build = (step) => {
    const tick = (base) => base + step * int(5, 400);
    let files = {
      ...machine(100000 + step * 4000, 50000 + step * int(500, 3500), 1000 + step * pick([0, 10, 400, 6000]), pick([15000000, 2000000, 900000, 3000000]), pick(['0.5', '13.0', '7.9', '25.4'])),
      ...proc(900, { comm: 'systemd', ppid: 1, uid: 0 }),
      ...proc(1000, { comm: 'node', ppid: 900, ticks: tick(500), rss: 300000, args: `node ${REL}/current/server.js`, exe: '/usr/bin/node' }),
      ...proc(1100, { comm: 'chrome', ppid: 1000, ticks: tick(100), rss: 500000, args: 'chrome --headless' }),
      ...proc(1500, { comm: 'tmux: server', ppid: 1, ticks: tick(50), args: 'tmux -L runtime new-session', exe: `${REL}/bin/tmux` }),
      ...proc(1501, { comm: 'bash', ppid: 1500, args: '-bash' }),
      ...proc(1502, { comm: 'zsh', ppid: 1500, args: 'zsh' }),
      ...proc(1600, { comm: 'claude', ppid: 1501, ticks: tick(3000), rss: 700000, args: 'claude --dangerously-skip-permissions' }),
      ...proc(1601, { comm: 'node', ppid: 1600, ticks: tick(10), rss: 90000, args: 'node /home/agent/mcp/server.js --api-key=sk-123' }),
      ...proc(1602, { comm: 'bash', ppid: 1600, args: 'bash -c npm test' }),
      ...proc(1603, { comm: 'node', ppid: 1602, ticks: tick(700), rss: 400000, args: 'node vite --port 5173', cwd: '/home/agent/app' }),
      ...proc(1700, { comm: 'node', ppid: 1502, ticks: tick(900), rss: 650000, args: 'node next dev', cwd: '/home/agent/site' }),
      ...proc(1701, { comm: 'python3', ppid: 1502, ticks: tick(200), rss: 120000, args: 'python3 -m http.server 8000 --token abc' }),
      ...proc(1800, { comm: 'sudo', ppid: 1502, uid: UID, euid: 0, rss: 9000, args: 'sudo ls' }),
      ...proc(1900, { comm: 'other user', ppid: 1, uid: 1001, rss: 999999 }),
      ...proc(2000, { comm: 'netbird', ppid: 900, rss: 80000, cgroup: '0::/system.slice/netbird.service' }),
      ...proc(2100, { comm: 'agent', ppid: 900, ticks: tick(80), rss: 200000, cgroup: '0::/system.slice/other-agent.service', args: '/opt/x/agent' }),
      ...proc(2101, { comm: 'helper', ppid: 2100, rss: 30000, cgroup: '0::/user.slice/u.scope' }),
      ...proc(2200, { comm: 'zombie', ppid: 1502, noRss: true }),
      ...proc(2300, { comm: 'we(ird) (name', ppid: 1502, ticks: tick(20), rss: 50000, args: '' }),
      ...proc(2400, { comm: 'node', ppid: 1, ticks: tick(10), rss: 40000, args: 'node /srv/app.js', cwd: `${REL}/current/.next/standalone` }),
    };
    for (let k = 0; k < 14; k += 1) files = { ...files, ...proc(3000 + k * 7, { comm: pick(['node', 'go', 'rg', 'esbuild', 'postgres']), ppid: pick([1, 1502, 1600, 3000]), ticks: tick(int(0, 900)), rss: int(1000, 900000), args: `${pick(['node', 'go', 'rg'])} ${genCommand()}` }) };
    if (step === 3) delete files['1700/stat'];
    if (step >= 4) files['1603/stat'] = files['1603/stat'].replace(/ 0 (\d+) 100000/, ' 0 9$1 100000');
    return files;
  };
  const materialize = (dir, files) => {
    for (const [name, content] of Object.entries(files)) {
      const file = path.join(dir, name);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      if (typeof content === 'object') fs.symlinkSync(content.link, file);
      else fs.writeFileSync(file, content);
    }
  };
  const steps = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'];
  steps.forEach((name, i) => { trees[name] = build(i); materialize(path.join(root, `proc-${name}`), trees[name]); });
  const point = (name) => { fs.rmSync(procLink, { force: true }); fs.symlinkSync(`proc-${name}`, procLink); };
  let next = 0;
  point(steps[next]);
  const portsOf = (fresh, responding) => [
    { port: 5173, pid: 1603, command: 'node', cwd: '/home/agent/app', kind: 'site', responding, group: 'other', owner: null, fresh },
    { port: 3000, pid: 1700, command: 'node', cwd: '/home/agent/site-listener', kind: 'site', responding: !responding, group: 'other', owner: null, fresh: true },
    { port: 8000, pid: 1701, command: 'python3', cwd: '/home/agent', kind: 'api', responding: false, group: 'other', owner: null, fresh },
    { port: 8001, pid: 1701, command: 'python3', cwd: '/home/agent', kind: 'api', responding: true, group: 'other', owner: null, fresh: true },
    { port: 9999, pid: 1900, command: null, cwd: null, kind: 'unknown', responding: false, group: 'other', owner: null, fresh: true },
    { port: 7000, pid: null, command: null, cwd: null, kind: 'unknown', responding: false, group: 'other', owner: null, fresh: true },
  ];
  let ports = portsOf(true, false);
  let portsGen = 1;
  const plan = [
    { advance: 0, ports: [true, false] },
    { advance: 3000, ports: [true, false] },
    { advance: 3000, reuse: true },
    { advance: 3000, ports: [false, false] },
    { advance: 3000, ports: [true, false] },
    { advance: 3000, ports: [true, true] },
    { advance: 15000, ports: [true, false] },
  ];
  const statfsSeq = [[1000, 100, 90], [1000, 50, 40], [1000, 20, 10], [0, 0, 0], [1000, 120, 100], [1000, 900, 900], [1000, 100, 90]];
  let scanIndex = 0;
  const monitor = scanMod.createMachineLoadMonitor({
    procRoot: procLink,
    uid: UID,
    runtimePid: 1000,
    cores: 4,
    homeDir: '/home/agent',
    ownerOf: async (pid) => { try { return Number(fs.readFileSync(path.join(procLink, String(pid), 'owner'), 'utf8')); } catch { return null; } },
    exeOf: async (pid) => { try { return fs.readlinkSync(path.join(procLink, String(pid), 'exe')); } catch { return null; } },
    cwdOf: async (pid) => { try { return fs.readlinkSync(path.join(procLink, String(pid), 'cwd')); } catch { return null; } },
    argsOf: async (pid) => { try { return fs.readFileSync(path.join(procLink, String(pid), 'cmdline'), 'utf8').split('\0').join(' ').trim(); } catch { return null; } },
    now: () => NOW,
    sleep: async (ms) => { NOW += ms; next += 1; point(steps[next]); },
    statfs: async () => { const [blocks, bfree, bavail] = statfsSeq[scanIndex]; return { blocks, bfree, bavail }; },
    listPorts: async () => ports,
    panePids: async () => [1501, 1502],
  });
  NOW = FIXED;
  for (const step of plan) {
    NOW += step.advance;
    if (step.advance) { next += 1; point(steps[next]); }
    if (!step.reuse) { ports = portsOf(...step.ports); portsGen += 1; }
    const result = await monitor.collect();
    loadScenario.scans.push({ advance: step.advance, reuse: !!step.reuse, at: NOW, tree: steps[next], ports: ports.map((p) => ({ port: p.port, pid: p.pid ?? 0, cwd: p.cwd ?? '', responding: p.responding, fresh: p.fresh !== false })),
      gen: portsGen, statfs: statfsSeq[scanIndex], result: JSON.stringify(result) });
    scanIndex += 1;
  }
  loadScenario.trees = trees;
  loadScenario.uid = UID;
  NOW = FIXED;
}

// --- the API through the dispatcher -----------------------------------------------------------------------------

const TOKEN = 'b'.repeat(64);
fs.writeFileSync(path.join(DATA, 'cli-token'), TOKEN, { mode: 0o600 });
const vapid = crypto.generateKeyPairSync('ec', { namedCurve: 'P-256' });
const jwk = vapid.privateKey.export({ format: 'jwk' });
const pub = Buffer.concat([Buffer.from([4]), Buffer.from(jwk.x, 'base64url'), Buffer.from(jwk.y, 'base64url')]).toString('base64url');
const fixtures = {
  'vapid-keys.json': JSON.stringify({ publicKey: pub, privateKey: jwk.d }, null, 2),
  'rate-limits.json': JSON.stringify({ claude: { ts: FIXED / 1000 - 30, five_hour: { used_percentage: 40, resets_at: FIXED / 1000 + 3000, at: FIXED / 1000 - 40, source: 'statusline' },
    seven_day: { used_percentage: 70, resets_at: FIXED / 1000 - 10 }, plan: { name: 'Max' }, pace: { five_hour: { z: 1, a: 2 } } }, codex: null }),
  'limits-history.jsonl': [0, 600, 1200, 1800, 2400, 3000, 3300].map((d, k) => JSON.stringify({ t: FIXED / 1000 - 3600 + d, p: 'claude', w: { five_hour: [k * 3, FIXED / 1000 + 3000, FIXED / 1000 - 3600 + d] } })).join('\n') + '\n',
};
for (const [name, content] of Object.entries(fixtures)) fs.writeFileSync(path.join(DATA, name), content, { mode: 0o600 });

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const routeList = ['/api/health', `/api/${P}/machines`, `/api/${P}/machine/load`, `/api/${P}/release-notice`, '/api/push/vapid-key', '/api/push/subscribe',
  '/api/push/register-session', '/api/push/diagnostic', '/api/push/viewport', '/api/push/test']
  .map((route) => ({ route, load: () => import(`@/pages${route}`) }));
const dispatch = createApiDispatcher(routeList);
const server = http.createServer((req, res) => { dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } }); });
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const baseURL = `http://127.0.0.1:${server.address().port}`;

for (const k of ['cpus', 'totalmem', 'freemem', 'loadavg', 'uptime', 'hostname', 'userInfo']) os[`real_${k}`] = os[k];
const host = { cpus: 6, totalmem: 33554432000, freemem: 12000000000, load: 2.37, uptime: 123456.6, hostname: `${P}-box1.example.org`, user: 'agent' };
os.cpus = () => Array.from({ length: host.cpus }, () => ({ model: 'x', speed: 1, times: {} }));
os.totalmem = () => host.totalmem;
os.freemem = () => host.freemem;
os.loadavg = () => [host.load, 1, 1];
os.uptime = () => host.uptime;
os.hostname = () => host.hostname;
os.userInfo = () => ({ username: host.user, uid: 1000 });

const actEvents = genEvents(Math.floor(FIXED / 1000) - 60);
const apiActivity = { claude: buildActivity(actEvents, Math.floor(FIXED / 1000) - 60), codex: null };
const neutralHeaders = (h) => Object.fromEntries(Object.entries(h).map(([k, v]) => [k === OLD_HEADER ? NEUTRAL.tokenHeader : k, v]));
const tok = { [OLD_HEADER]: TOKEN };
const ses = { cookie: 'session-token=good' };
const json = { 'content-type': 'application/json' };
const sub = (n, extra = {}) => JSON.stringify({ subscription: { endpoint: `https://push.example.org/${n}`, keys: { p256dh: `k${n}`, auth: `a${n}`, extra: 1 } }, ...extra });
const M = `/api/${P}/machines`;
const requests = [
  ['GET', '/api/health', {}],
  ['GET', '/agents/api/health', { 'if-none-match': 'ETAG' }],
  ['GET', M, {}],
  ['GET', M, tok],
  ['GET', M, ses, undefined, { activity: null }],
  ['GET', M, { ...ses, 'if-none-match': 'ETAG' }, undefined, { activity: null }],
  ['POST', `/agents${M}?machine=x`, { ...ses, ...json }, '{}', { activity: 'claude' }],
  ['GET', M, { ...ses, ...json }, '{bad', { activity: null }],
  ['GET', `/api/${P}/machine/load`, tok],
  ['POST', `/api/${P}/machine/load`, ses],
  ['GET', `/api/${P}/release-notice`, tok],
  ['GET', `/api/${P}/release-notice`, ses],
  ['POST', `/api/${P}/release-notice`, { ...ses, ...json }, '{"build":"bad build"}'],
  ['POST', `/api/${P}/release-notice`, { ...ses, ...json }, '{"build":"b-1","version":"1.2.3"}'],
  ['POST', `/api/${P}/release-notice`, { ...ses, ...json }, '{"build":"b-1","version":"x"}'],
  ['POST', `/api/${P}/release-notice`, { ...ses, ...json }, '{"build":"b-2","version":"x"}'],
  ['GET', '/api/push/vapid-key', ses],
  ['POST', '/api/push/vapid-key', ses],
  ['GET', '/api/push/subscribe', tok],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, '{"subscription":{"endpoint":"http://x/1","keys":{"p256dh":"a","auth":"b"}}}'],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, '{"endpoint":"https://x/1","keys":{"p256dh":"","auth":"b"}}'],
  ['POST', '/api/push/subscribe', { ...ses }, 'text'],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, sub(1, { deviceId: 'laptop', mobile: false })],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, sub(1, { deviceId: 'laptop', mobile: false })],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, sub(2, { deviceId: 'phone', mobile: true })],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, JSON.stringify({ endpoint: 'https://push.example.org/3', keys: { auth: 'z', p256dh: 'y' }, deviceId: '' })],
  ['POST', '/api/push/subscribe', { ...ses, ...json }, sub(4, { deviceId: 'laptop' })],
  ['GET', '/api/push/subscribe', ses],
  ['DELETE', '/api/push/subscribe', { ...ses, ...json }, '{}'],
  ['DELETE', '/api/push/subscribe', { ...ses, ...json }, '{"endpoint":5}'],
  ['DELETE', '/api/push/subscribe', { ...ses, ...json }, '{"endpoint":"https://push.example.org/2"}'],
  ['PUT', '/api/push/subscribe', ses],
  ['GET', '/api/push/subscribe', ses],
  ['POST', '/api/push/register-session', { ...ses, ...json }, '{"sessionId":"s1","endpoint":"https://x"}'],
  ['POST', '/api/push/register-session', { ...ses, ...json }, '{"sessionId":""}'],
  ['POST', '/api/push/register-session', { ...ses, ...json }, '{"sessionId":7}'],
  ['GET', '/api/push/register-session', ses],
  ['POST', '/api/push/diagnostic', { ...ses, ...json }, '{"step":"subscribe","message":"denied"}'],
  ['GET', '/api/push/diagnostic', ses],
  ['POST', '/api/push/viewport', { ...ses, ...json }, '{"sizes":{"w":390.123}}'],
  ['GET', '/api/push/viewport', ses],
  ['POST', '/api/push/test', { ...ses, ...json }, '{"endpoint":"https://push.example.org/none"}'],
  ['GET', '/api/push/test', ses],
  ['POST', '/api/push/subscribe', ses],
];
const KEEP = ['content-type', 'etag', 'allow', 'cache-control'];
const api = [];
let lastEtag = '';
for (const [method, url, headers, body, opts] of requests) {
  globalThis.__golden = { activity: opts?.activity === 'claude' ? apiActivity : null };
  const h = { ...headers };
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const res = await new Promise((resolve, reject) => {
    const req = http.request(baseURL + url, { method, headers: body === undefined ? h : { ...h, 'content-length': Buffer.byteLength(body) } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end(body);
  });
  if (res.headers.etag) lastEtag = res.headers.etag;
  const answer = scrubText(res.text);
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = String(res.headers[k]);
  if (kept.etag && answer !== res.text) kept.etag = etagOf(answer);
  api.push({ method, url: scrubText(url), headers: neutralHeaders(h), body: body ?? null, activity: opts?.activity ?? null, status: res.status,
    answerHeaders: kept, answer, conditional: headers['if-none-match'] === 'ETAG' });
}
server.close();
const dataFiles = {};
for (const name of ['push-subscriptions.json', 'release-notice.json']) {
  dataFiles[name] = fs.existsSync(path.join(DATA, name)) ? fs.readFileSync(path.join(DATA, name), 'utf8') : null;
}

const target = process.env.GOLDEN_OUT || path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'golden.json');
fs.writeFileSync(target, JSON.stringify({
  fixed: FIXED, forecasts, limits: limitsCases, historySteps, transcripts, redact, agents: agentCases, units,
  services: JSON.parse(scrubText(JSON.stringify(services))), load: JSON.parse(scrubText(JSON.stringify(loadScenario))),
  api: { token: TOKEN, host: { ...host, hostname: scrubText(host.hostname) }, fixtures, events: actEvents, activityAsOf: Math.floor(FIXED / 1000) - 60,
    release: { version: '1.42.0', commit: 'abc1234' }, requests: api, files: dataFiles },
}) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`forecasts ${forecasts.length}, limits ${limitsCases.length}, redact ${redact.length}, load scans ${loadScenario.scans.length}, api ${api.length} → ${target}`);
