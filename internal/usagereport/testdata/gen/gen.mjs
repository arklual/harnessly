// Sends requests to the previous runtime's own API dispatcher and its usage-report handler, on fixture transcripts, and
// writes what each answered, and the facts file it left, to ../golden.json. The Go test lays out the same files, sends
// the same requests and must answer alike.
//
// The previous runtime hard-codes its workspace folder's name and its tracker's task prefixes; the fixtures carry
// placeholders ({ROOT}, {P1}..{P3}, {p1}..{p3} in lower case) that are filled with those words for the run and put
// back in what it answered, so the Go test fills them with its own.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_MODULE_PREFIX=<prefix of its module names> \
//   OLD_RUNTIME_ROOT_NAME=<its workspace folder> OLD_RUNTIME_TASK_PREFIXES=<A,B,C> \
//   node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated transcripts, the facts of each and reports over groups of
// them instead (TS_GOLDEN of the Go test).
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

process.env.TZ = 'UTC';
let NOW = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(NOW); }
  static now() { return NOW; }
};
const M = process.env.OLD_RUNTIME_MODULE_PREFIX;
const ROOT_NAME = process.env.OLD_RUNTIME_ROOT_NAME;
const PREFIXES = (process.env.OLD_RUNTIME_TASK_PREFIXES ?? '').split(',').filter(Boolean);
if (!M || !ROOT_NAME || PREFIXES.length !== 3) throw new Error('OLD_RUNTIME_MODULE_PREFIX, OLD_RUNTIME_ROOT_NAME and OLD_RUNTIME_TASK_PREFIXES (three) are required');
const fill = (s) => s.replaceAll('{ROOT}', ROOT_NAME)
  .replaceAll('{P1}', PREFIXES[0]).replaceAll('{P2}', PREFIXES[1]).replaceAll('{P3}', PREFIXES[2])
  .replaceAll('{p1}', PREFIXES[0].toLowerCase()).replaceAll('{p2}', PREFIXES[1].toLowerCase()).replaceAll('{p3}', PREFIXES[2].toLowerCase());
const unfill = (s) => {
  let out = s.replaceAll(ROOT_NAME, '{ROOT}');
  PREFIXES.forEach((p, i) => { out = out.replaceAll(p, `{P${i + 1}}`).replaceAll(p.toLowerCase(), `{p${i + 1}}`); });
  return out;
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'usage-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
delete process.env.CLAUDE_CONFIG_DIR;
delete process.env.CODEX_HOME;
os.hostname = () => 'box';
os.userInfo = () => ({ username: 'agent' });
// The worker script is looked up under the working folder; there is none here, so the report is built inline.
process.chdir(root);
const scrub = (s) => unfill(s.split(root).join('$ROOT'));
const j = (rows) => rows.map((r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n';
const sec = (iso) => Math.floor(Date.parse(iso) / 1000);

// --- generated transcripts ------------------------------------------------------------------------------------------
if (process.env.GEN_RANDOM) {
  const facts = await import(`@/lib/${M}usage/facts`);
  const report = await import(`@/lib/${M}usage/report`);
  let seed = 23;
  const rnd = () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  const int = (n) => Math.floor(rnd() * n);
  const count = () => pick([0, 0, 1, 5, 120, 3000, 45678, 1.5, -3, '7', null, undefined, 1e6, 2.25]);
  const stamp = () => pick(['2026-10-17T10:00:00.000Z', '2026-10-17T10:05:30Z', '2026-10-16T23:59:59.999Z', '2026-10-17T21:30:00Z',
    '2025-01-01T00:00:00Z', '2024-01-01T00:00:00Z', 'bad', 5, undefined, null, '2026-10-17', '2026-09-30T12:00:00+03:00']);
  const ids = ['msg_1', 'msg_2', 'msg_3', 'msg_4', '', 0, 12, true, null, undefined, { a: 1 }, [1, 2]];
  const uuids = ['u1', 'u2', 'u3', 'u4', 'u5', undefined, null, 7, ''];
  const reqs = ['req_1', 'req_2', undefined, 5, ''];
  const models = ['claude-opus-4-6', 'claude-sonnet-4-5-20250929', 'claude-haiku-4-5', 'claude-opus-5-5', 'claude-fable-5-1', '<synthetic>', '',
    'claude-unknown-9', 'opus-like', 'Haiku-Thing', undefined, null];
  const badModels = [7, true, { m: 1 }, ['claude-opus-4-6']];
  const codexModels = ['gpt-5.4', 'gpt-5.4-mini', 'GPT-5.1-Codex', 'openai/gpt-5', 'o3', '', 'gpt-5.2-codex-20260101', 5, null];
  const cwds = [undefined, null, '', 5, '/nonexistent-r/{ROOT}/app', '/nonexistent-r/{ROOT}/lib/src', '/nonexistent-r/{ROOT}/.worktrees/app-{p1}-12',
    '/nonexistent-r/{ROOT}/.worktrees/web-w3', '/nonexistent-r/{ROOT}/.worktrees/x-0a1b2c3d', '/nonexistent-r/{ROOT}/.claude/worktrees/zz',
    '/nonexistent-r/{ROOT}', '/nonexistent-r/{ROOT}/.wt', '/nonexistent-r/other/place', '/nonexistent-r/.worktrees/{ROOT}/q',
    '/nonexistent-r/{ROOT}/.worktrees/-12', '/nonexistent-r/workspace/{ROOT}/svc', '/nonexistent-r/{ROOT}/.worktrees/api-{P2}1988'];
  const texts = ['hello', '  ', '', 'fix {P1}-12 and {P3}-1234567', '<system-reminder>x</system-reminder>', '  <command-name>/x',
    '[Request interrupted by user]', '[Request interrupted by user for tool use]', 'see {P2}-77, {P2}-77 again', 'x'.repeat(250),
    'emoji 😀'.repeat(40), '<bash-stdout>out</bash-stdout>', 'a b  \t c', "The user doesn't want to proceed with this tool use. ok",
    '<teammate-message>hi</teammate-message>', 'text with "type":"progress" inside'];
  const content = () => pick([
    () => pick(texts),
    () => [{ type: 'text', text: pick(texts) }, { type: 'text', text: pick(texts) }],
    () => [{ type: 'image' }, { type: 'text', text: 5 }],
    () => [{ type: 'tool_result', tool_use_id: pick(['t1', 't2', 't3', undefined, null, 9, { o: 1 }]), is_error: pick([true, false, 'true', undefined]),
      content: pick([pick(texts), [{ type: 'text', text: pick(texts) }], 5, null]) }],
    () => 5,
    () => null,
  ])();
  const claudeLine = () => {
    const base = {};
    if (rnd() < 0.7) base.timestamp = stamp();
    if (rnd() < 0.6) base.uuid = pick(uuids);
    if (rnd() < 0.5) base.cwd = pick(cwds);
    if (rnd() < 0.2) base.sessionId = pick(['sess-a', 'sess-b', 5]);
    if (rnd() < 0.15) base.gitBranch = pick(['feature/{P1}-5', 'main', 7, '{P3}-99-x']);
    if (rnd() < 0.1) base.isSidechain = pick([true, false, 'true']);
    switch (int(14)) {
      case 0: case 1: case 2: case 3: {
        const usage = { input_tokens: count(), output_tokens: count(), cache_read_input_tokens: count(), cache_creation_input_tokens: count() };
        if (rnd() < 0.5) usage.cache_creation = pick([{ ephemeral_5m_input_tokens: count(), ephemeral_1h_input_tokens: count() }, {}, null, 'x']);
        const bad = rnd() < 0.004;
        const message = { id: pick(ids), model: bad ? pick(badModels) : pick(models), usage: rnd() < 0.05 ? pick([null, 0, 'u', [1]]) : usage };
        if (rnd() < 0.2) delete message.id;
        const e = { ...base, type: 'assistant', requestId: pick(reqs), message };
        if (rnd() < 0.05) e.isApiErrorMessage = pick([true, 'true']);
        if (bad) e.timestamp = '2026-10-17T11:00:00.000Z';
        return JSON.stringify(e);
      }
      case 4: return JSON.stringify({ ...base, type: 'system', subtype: pick(['turn_duration', 'compact_boundary', 'other']), durationMs: count() });
      case 5: case 6: case 7: {
        const e = { ...base, type: 'user', message: pick([{ content: content() }, null, 'x', { content: content(), role: 'user' }]) };
        if (rnd() < 0.1) e.isMeta = pick([true, false]);
        if (rnd() < 0.05) e.isCompactSummary = true;
        return JSON.stringify(e);
      }
      case 8: return JSON.stringify({ ...base, type: 'attachment', attachment: pick([{ type: 'queued_command', prompt: pick(texts) },
        { type: 'queued_command', prompt: [{ type: 'text', text: pick(texts) }] }, { type: 'other' }, null]) });
      case 9: return JSON.stringify({ ...base, type: 'ai-title', aiTitle: pick(['A  title\nhere ', 5, '', 'Second title']) });
      case 10: return JSON.stringify({ ...base, type: 'pr-link', prUrl: pick(['https://git.example/x/-/merge_requests/1', 'https://git.example/x/-/merge_requests/2', 3]) });
      case 11: return pick(['{"type":"assistant"', '"user"', '[1,2]', '5', '', '   ', '{"type":"file-history-snapshot","x":1}', '{"type":"progress","data":1}', 'true']);
      case 12: return rnd() < 0.01 ? 'null' : JSON.stringify({ ...base, type: 'summary' });
      default: return JSON.stringify({ ...base, type: pick(['user', 'assistant']) });
    }
  };
  const codexLine = (st) => {
    const ts = rnd() < 0.9 ? stamp() : undefined;
    switch (int(14)) {
      case 0: return JSON.stringify({ type: 'session_meta', payload: { id: pick(['c-1', 'c-2', 5]), cwd: pick(cwds), thread_source: pick([undefined, 'user', 'subagent', 3]) } });
      case 1: return JSON.stringify({ type: 'turn_context', payload: { model: pick(codexModels), cwd: pick(cwds) } });
      case 2: case 3: case 4: {
        if (rnd() < 0.15) st.c = { i: 0, c: 0, o: 0, w: 0 };
        if (rnd() < 0.7) { st.c.i += int(5000); st.c.c += int(3000); st.c.o += int(800); st.c.w += int(50); }
        const total = { input_tokens: st.c.i, cached_input_tokens: st.c.c, output_tokens: st.c.o };
        if (rnd() < 0.3) total.cache_write_input_tokens = st.c.w;
        return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'token_count', info: rnd() < 0.1 ? pick([null, {}, { total_token_usage: 'x' }]) : { total_token_usage: total } } });
      }
      case 5: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'user_message', message: pick([...texts, 5, null, undefined, ['a', 'b']]) } });
      case 6: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'item_completed', item: pick([{ type: 'UserMessage', id: pick(['i1', 'i2', 4, null]), content: content() }, { type: 'Other' }, null]) } });
      case 7: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'task_started', turn_id: pick(['t1', 't2', 4]) } });
      case 8: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'task_complete', turn_id: pick(['t1', 't2', 4, undefined]) } });
      case 9: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: pick(['turn_aborted', 'context_compacted']), turn_id: pick(['t1', undefined]) } });
      case 10: return pick(['{"type":"event_msg"', '5', '', '{"type":"event_msg","payload":5}', '{"type":"event_msg","payload":null}']);
      case 11: return rnd() < 0.03 ? 'null' : JSON.stringify({ type: 'response_item', payload: {} });
      default: return JSON.stringify({ type: 'event_msg', timestamp: ts, payload: { type: 'agent_message' } });
    }
  };
  const n = Number(process.env.GEN_RANDOM);
  const files = [];
  const outFacts = {};
  for (let i = 0; i < n; i++) {
    const codex = rnd() < 0.35;
    const rel = codex ? `home/.codex/sessions/2026/10/17/rollout-${i}.jsonl`
      : pick([`home/.claude/projects/-p/s${i}.jsonl`, `home/.claude/projects/-p/s${i % 7}/subagents/agent-${i}.jsonl`]);
    const st = { c: { i: 0, c: 0, o: 0, w: 0 } };
    const lines = Array.from({ length: 1 + int(30) }, () => (codex ? codexLine(st) : claudeLine()));
    const text = fill(lines.join(pick(['\n', '\n', '\r\n', '\n\n'])) + pick(['\n', '', '\r']));
    const file = path.join(root, rel);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, text);
    files.push({ path: rel, content: scrub(text) });
    try {
      const f = codex ? await facts.parseCodexFile(file) : await facts.parseClaudeFile(file);
      outFacts[rel] = JSON.parse(scrub(JSON.stringify(f)));
    } catch (error) {
      outFacts[rel] = { error: String(error).slice(0, 40) };
    }
  }
  const reports = [];
  const okFiles = files.filter((f) => !outFacts[f.path].error);
  for (let at = 0; at < okFiles.length; at += 25) {
    const group = okFiles.slice(at, at + 25).map((f) => f.path);
    let out;
    try {
      out = scrub(JSON.stringify(report.buildReport(group.map((p) => JSON.parse(fill(JSON.stringify(outFacts[p])))), { person: 'p@example.com', machine: 'box' })));
    } catch (error) {
      out = `error: ${String(error).slice(0, 40)}`;
    }
    reports.push({ files: group, report: out });
  }
  fs.writeFileSync(process.env.GEN_OUT, JSON.stringify({ now: NOW, files, facts: outFacts, reports }));
  fs.rmSync(root, { recursive: true, force: true });
  process.exit(0);
}

// --- the route --------------------------------------------------------------------------------------------------------
const OLD_HEADERS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',').filter(Boolean);
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
if (OLD_HEADERS.length !== 2 || !OLD_API) throw new Error('OLD_RUNTIME_TOKEN_HEADERS (two names) and OLD_RUNTIME_PLATFORM_API are required');
const NEUTRAL_HEADERS = ['x-cli-token', 'x-old-cli-token'];
const TOKEN = 'c'.repeat(64);

const { dataDir } = await import('@/lib/data-dir');
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const DATA = dataDir();
fs.writeFileSync(path.join(DATA, 'cli-token'), TOKEN, { mode: 0o600 });
const CACHE = path.join(DATA, 'stats', `${M}usage-v2.json`);

const PROJ = 'home/.claude/projects/-w-app';
const CODEX = 'home/.codex/sessions/2026/10/17';
const S1 = '5d3c1e0a-1111-4a2b-9c3d-000000000001';
const S2 = '5d3c1e0a-2222-4a2b-9c3d-000000000002';
const APP = '$ROOT/ws/{ROOT}/app';
const asst = (ts, req, id, model, usage, extra = {}) => ({ type: 'assistant', timestamp: ts, requestId: req, cwd: APP, sessionId: S1, message: { id, model, role: 'assistant', usage }, ...extra });
const user = (ts, uuid, content, extra = {}) => ({ type: 'user', timestamp: ts, uuid, cwd: APP, sessionId: S1, message: { role: 'user', content }, ...extra });
const main = [
  { type: 'summary', summary: 'x' },
  { type: 'file-history-snapshot', snapshot: { type: 'user' } },
  user('2026-10-17T09:00:00.000Z', 'p1', 'Start on {P1}-101 please', { gitBranch: 'feature/{P2}-7' }),
  asst('2026-10-17T09:00:05.000Z', 'r1', 'm1', 'claude-opus-4-6', { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 1000, cache_creation_input_tokens: 400 }),
  asst('2026-10-17T09:00:06.000Z', 'r1', 'm1', 'claude-opus-4-6', { input_tokens: 10, output_tokens: 250, cache_read_input_tokens: 1000, cache_creation_input_tokens: 400,
    cache_creation: { ephemeral_5m_input_tokens: 100, ephemeral_1h_input_tokens: 300 } }),
  asst('2026-10-17T09:00:07.000Z', 'r2', 'm2', '<synthetic>', { input_tokens: 9 }),
  asst('2026-10-17T09:00:08.000Z', 'r3', 'm3', 'claude-opus-4-6', { input_tokens: 9 }, { isApiErrorMessage: true }),
  user('2026-10-17T09:01:00.000Z', 'tr1', [{ type: 'tool_result', tool_use_id: 'tool-1', content: 'ok' }, { type: 'tool_result', tool_use_id: 'tool-2', is_error: true,
    content: [{ type: 'text', text: "The user doesn't want to proceed with this tool use." }] }]),
  user('2026-10-17T09:02:00.000Z', 'p2', '<system-reminder>harness</system-reminder>'),
  user('2026-10-17T09:03:00.000Z', 'p3', '[Request interrupted by user]'),
  user('2026-10-17T09:04:00.000Z', 'p4', [{ type: 'text', text: 'second prompt about {P3}-5' }, { type: 'image' }]),
  user('2026-10-17T09:04:30.000Z', 'p5', 'meta prompt', { isMeta: true }),
  { type: 'attachment', timestamp: '2026-10-17T09:05:00.000Z', uuid: 'q1', cwd: APP, attachment: { type: 'queued_command', prompt: 'queued {P1}-202' } },
  { type: 'system', subtype: 'turn_duration', durationMs: 65000, timestamp: '2026-10-17T09:06:00.000Z', uuid: 'turn1' },
  { type: 'system', subtype: 'compact_boundary', timestamp: '2026-10-17T09:07:00.000Z', uuid: 'cb1' },
  { type: 'ai-title', aiTitle: 'Fix   the\nlogin  flow ' },
  { type: 'pr-link', prUrl: 'https://git.example/acme/app/-/merge_requests/9' },
  asst('2026-10-17T21:30:00.000Z', 'r4', 'm4', 'claude-sonnet-4-5-20250929', { input_tokens: 3000, output_tokens: 700, cache_read_input_tokens: 20000 }),
  asst('2025-01-01T00:00:00.000Z', 'r5', 'm5', 'claude-opus-4-6', { input_tokens: 5 }),
  user('2026-10-17T22:10:00.000Z', 'p6', 'mentions "type":"progress" so it is skipped'),
  '{"type":"user","broken":',
];
const resumed = [
  // A resumed copy: the original's lines again under the new session, then new work in a worktree of app.
  ...main.slice(2, 5).map((e) => ({ ...e, sessionId: S2 })),
  user('2026-10-17T23:00:00.000Z', 'p20', 'continue on {P1}-101', { sessionId: S2, cwd: '$ROOT/ws/{ROOT}/.worktrees/app-{p1}-55' }),
  asst('2026-10-17T23:00:10.000Z', 'r20', 'm20', 'claude-haiku-4-5', { input_tokens: 50, output_tokens: 60 }, { sessionId: S2, cwd: '$ROOT/ws/lib-wt' }),
  user('2026-10-17T23:01:00.000Z', 'p21', 'in another place', { sessionId: S2, cwd: '/nonexistent-golden/elsewhere' }),
  user('2026-10-17T23:02:00.000Z', 'p22', 'at the root', { sessionId: S2, cwd: '/nonexistent-golden/{ROOT}' }),
  user('2026-10-17T23:03:00.000Z', 'p23', 'in a hashed worktree', { sessionId: S2, cwd: '/nonexistent-golden/{ROOT}/.claude/feat-0a1b2c3d' }),
];
const sub = [
  { type: 'user', timestamp: '2026-10-17T09:10:00.000Z', uuid: 'sp1', isSidechain: true, sessionId: S1, cwd: APP, message: { content: 'subagent task' } },
  { type: 'assistant', timestamp: '2026-10-17T09:10:05.000Z', requestId: 'rs1', isSidechain: true, sessionId: S1, cwd: APP,
    message: { id: 'ms1', model: 'claude-haiku-4-5-20251001', usage: { input_tokens: 800, output_tokens: 90 } } },
  { type: 'system', subtype: 'turn_duration', durationMs: 5000, timestamp: '2026-10-17T09:10:06.000Z', uuid: 'turn-s' },
];
const codexA = [
  { type: 'session_meta', timestamp: '2026-10-17T12:00:00.000Z', payload: { id: 'cx-1', cwd: '$ROOT/ws/{ROOT}/app' } },
  { type: 'turn_context', payload: { model: 'gpt-5.4', cwd: '$ROOT/ws/{ROOT}/app' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:00:01.000Z', payload: { type: 'task_started', turn_id: 'T1' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:00:02.000Z', payload: { type: 'user_message', message: 'codex prompt for {P2}-3' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:00:03.000Z', payload: { type: 'token_count', info: null } },
  { type: 'event_msg', timestamp: '2026-10-17T12:00:04.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 10000, cached_input_tokens: 6000, output_tokens: 500 } } } },
  { type: 'event_msg', timestamp: '2026-10-17T12:00:05.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 10000, cached_input_tokens: 6000, output_tokens: 500 } } } },
  { type: 'event_msg', timestamp: '2026-10-17T12:01:00.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 25000, cached_input_tokens: 20000, output_tokens: 900, cache_write_input_tokens: 40 } } } },
  { type: 'event_msg', timestamp: '2026-10-17T12:02:00.000Z', payload: { type: 'task_complete', turn_id: 'T1' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:03:00.000Z', payload: { type: 'item_completed', item: { type: 'UserMessage', id: 'it-1', content: [{ type: 'text', text: 'second codex prompt' }] } } },
  { type: 'event_msg', timestamp: '2026-10-17T12:04:00.000Z', payload: { type: 'turn_aborted', turn_id: 'T2' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:05:00.000Z', payload: { type: 'context_compacted' } },
  { type: 'turn_context', payload: { model: 'gpt-5.4-mini' } },
  { type: 'event_msg', timestamp: '2026-10-17T12:06:00.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 3000, cached_input_tokens: 1000, output_tokens: 100 } } } },
];
const codexSub = [
  { type: 'session_meta', timestamp: '2026-10-17T13:00:00.000Z', payload: { id: 'cx-1', thread_source: 'subagent' } },
  { type: 'event_msg', timestamp: '2026-10-17T13:00:01.000Z', payload: { type: 'user_message', message: 'replayed prompt' } },
  { type: 'event_msg', timestamp: '2026-10-17T13:00:02.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 400, output_tokens: 20 } } } },
];
const T0 = sec('2026-10-17T23:30:00.000Z');
const files = [
  { path: `${PROJ}/${S1}.jsonl`, content: j(main), mtime: T0 },
  { path: `${PROJ}/${S1}/subagents/agent-a1.jsonl`, content: j(sub), mtime: T0 - 60 },
  { path: `${PROJ}/${S2}.jsonl`, content: j(resumed), mtime: T0 - 120 },
  { path: `${PROJ}/old.jsonl`, content: j([user('2025-06-01T00:00:00.000Z', 'o1', 'ancient prompt')]), mtime: sec('2025-06-01T00:00:00.000Z') },
  { path: `${PROJ}/broken.jsonl`, content: j([user('2026-10-17T08:00:00.000Z', 'b1', 'lost prompt'), 'null']), mtime: T0 - 180 },
  { path: `${PROJ}/notes.json`, content: '{}', mtime: T0 },
  { path: 'home/.claude/projects/a/b/c/d/deep.jsonl', content: j([user('2026-10-17T08:30:00.000Z', 'd1', 'too deep')]), mtime: T0 },
  { path: `${CODEX}/rollout-a.jsonl`, content: j(codexA), mtime: T0 - 240 },
  { path: `${CODEX}/rollout-sub.jsonl`, content: j(codexSub), mtime: T0 - 300 },
];
const P = (p) => path.join(root, p);
const write = (f) => {
  const file = P(f.path);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, fill(f.content.split('$ROOT').join(root)));
  fs.utimesSync(file, f.mtime, f.mtime);
};
files.forEach(write);
// The git side of the projects: a repository, and a worktree whose .git file points into another repository.
fs.mkdirSync(P(fill('ws/{ROOT}/app/.git')), { recursive: true });
fs.mkdirSync(P('ws/lib-wt'), { recursive: true });
fs.writeFileSync(P('ws/lib-wt/.git'), `gitdir: ${P(fill('ws/{ROOT}/lib/.git/worktrees/lib-wt'))}\n`);
fs.symlinkSync(P(`${PROJ}/${S2}.jsonl`), P(`${PROJ}/link.jsonl`));

const dispatch = createApiDispatcher([{ route: `${OLD_API}/stats`, load: () => import(`@/pages${OLD_API}/stats`) }]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const ses = (who) => ({ cookie: who ? `session-token=sub:${who}` : 'session-token=good' });
const tok = { 'x-cli-token': TOKEN };
const E = '/api/platform/stats';
const later = [
  { type: 'event_msg', timestamp: '2026-10-17T23:40:00.000Z', payload: { type: 'user_message', message: 'late codex prompt' } },
  { type: 'event_msg', timestamp: '2026-10-17T23:40:05.000Z', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 9000, cached_input_tokens: 1000, output_tokens: 300 } } } },
];
const fresh = { path: `${PROJ}/fresh.jsonl`, content: j([user('2026-10-17T23:50:00.000Z', 'f1', 'a new conversation', { sessionId: 'fresh-1', cwd: '$ROOT/ws/{ROOT}/app/sub' }),
  asst('2026-10-17T23:50:02.000Z', 'rf', 'mf', 'claude-opus-4-6', { input_tokens: 1, output_tokens: 2 }, { sessionId: 'fresh-1' })]), mtime: T0 + 1200 };
// Steps: a request, or an action on the files or the clock between requests (the Go test does the same).
const steps = [
  { req: ['GET', E, [], {}] },
  { req: ['GET', E, [], tok] },
  { req: ['GET', E, [], ses('dev@example.com')] },
  { req: ['GET', E, [['shape', 'report']], ses()] },
  { req: ['GET', E, [['shape', 'report']], { ...ses(), 'if-none-match': 'ETAG' }] },
  { append: `${CODEX}/rollout-a.jsonl`, content: j(later), mtime: T0 + 600 },
  { clock: 30 },
  { req: ['GET', E, [['wait', '0']], ses('dev@example.com')] },
  { clock: 31 },
  { req: ['GET', E, [['shape', 'report']], ses('dev@example.com')] },
  { remove: `${PROJ}/${S1}/subagents/agent-a1.jsonl` },
  { add: fresh },
  { clock: 61 },
  { req: ['POST', '/agents' + E + '/', [], ses('nobody')] },
  { req: ['GET', E, [['shape', 'report'], ['shape', 'report'], ['wait', '0'], ['wait', '0']], { ...ses('dev@example.com'), ...tok }] },
];

const KEEP = ['content-type', 'cache-control', 'allow'];
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => [NEUTRAL_HEADERS.includes(k) ? OLD_HEADERS[NEUTRAL_HEADERS.indexOf(k)] : k, v]));
const cacheText = () => (fs.existsSync(CACHE) ? fs.readFileSync(CACHE, 'utf8') : '');
let seen = cacheText();
const out = [];
let lastEtag = '';
for (const step of steps) {
  if (!step.req) {
    if (step.clock) NOW += step.clock * 1000;
    if (step.append) { fs.appendFileSync(P(step.append), fill(step.content)); fs.utimesSync(P(step.append), step.mtime, step.mtime); }
    if (step.remove) fs.rmSync(P(step.remove));
    if (step.add) write(step.add);
    out.push(step.add ? { add: step.add.path, content: step.add.content, mtime: step.add.mtime } : step);
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
  const now = cacheText();
  const written = now !== seen ? scrub(now) : '';
  seen = now;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = String(res.headers[k]);
  out.push({ method, path: urlPath, query, headers, status: res.status, answerHeaders: kept, etag: !!res.headers.etag, answer: scrub(res.text), cacheWritten: written });
}
server.close();
const neutral = files.map((f) => ({ ...f, content: f.content }));
const here = path.dirname(new URL(import.meta.url).pathname);
fs.writeFileSync(path.join(here, '..', 'golden.json'), `${JSON.stringify({ now: Date.parse('2026-10-18T00:00:00.000Z'), files: neutral, steps: out }, null, 1)}\n`);
fs.rmSync(root, { recursive: true, force: true });
