// Sends requests to the previous runtime's own API dispatcher and its snapshot, link and share-read handlers, on
// fixture tabs and transcripts with a clock it moves, and writes what each answered and the share files it left to
// ../golden.json. The Go test lays out the same files, sends the same requests at the same times and must answer and
// write byte for byte.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_MODULE_PREFIX=<prefix of its module names> \
//   OLD_RUNTIME_ENV_PREFIX=<prefix of its environment variables> OLD_RUNTIME_SHARE_HEADERS=<base header,token header> \
//   OLD_RUNTIME_SHARE_SALT=<HKDF salt> OLD_RUNTIME_SHARE_AUDIENCE=<token audience> OLD_RUNTIME_BRAND=<page brand> \
//   OLD_RUNTIME_FILE_PREFIX=<download name prefix> OLD_RUNTIME_PROFILE_HOMES=<profiles folder> \
//   node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// With GEN_RANDOM=<n> GEN_OUT=<file> it writes n generated cases of the pure parts (redaction, projection, page and
// Markdown of generated snapshots) instead, for the Go test's SHARE_RANDOM run; GEN_DROP_HOST is the host suffix whose
// links the page drops (the Go test takes it as SHARE_DROP_HOST). The brand, prefixes, salt and audience
// are replaced in what is written by neutral ones.
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import module from 'node:module';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const need = (name) => {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is required`);
  return v;
};
const M = need('OLD_RUNTIME_MODULE_PREFIX');
const OLD_API = need('OLD_RUNTIME_PLATFORM_API');
const OLD_HEADERS = need('OLD_RUNTIME_TOKEN_HEADERS').split(',');
const ENV_PREFIX = need('OLD_RUNTIME_ENV_PREFIX');
const [OLD_BASE_HEADER, OLD_TOKEN_HEADER] = need('OLD_RUNTIME_SHARE_HEADERS').split(',');
const OLD_SALT = need('OLD_RUNTIME_SHARE_SALT');
const OLD_AUDIENCE = need('OLD_RUNTIME_SHARE_AUDIENCE');
const OLD_BRAND = need('OLD_RUNTIME_BRAND');
const OLD_FILE_PREFIX = need('OLD_RUNTIME_FILE_PREFIX');
const OLD_PROFILE_HOMES = need('OLD_RUNTIME_PROFILE_HOMES');
// What the committed file says instead (the Go test is configured with these); the profiles folder keeps the length
// of the real one, so byte counts in what is written stay true.
const NEUTRAL = {
  headers: ['x-cli-token', 'x-old-cli-token'], base: 'x-share-base', token: 'x-share-token', salt: 'acme', audience: 'acme-share-read',
  brand: 'Acme Agents', filePrefix: 'acme', profileHomes: '/var/lib/acme-agent/profiles',
};

// --- a clock the steps move, and ids from a counter -------------------------------------------------------------------
process.env.TZ = 'UTC';
globalThis.__now = Date.parse('2026-10-18T09:30:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(globalThis.__now); }
  static now() { return globalThis.__now; }
};
let idCounter = 0;
const realRandomBytes = crypto.randomBytes;
crypto.randomBytes = (size, cb) => {
  if (size !== 32 || cb) return realRandomBytes(size, cb);
  idCounter++;
  return Buffer.alloc(32, idCounter);
};
module.syncBuiltinESMExports();

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'share-golden-')));
const home = path.join(root, 'home');
const workspace = path.join(home, 'work', 'acme');
fs.mkdirSync(workspace, { recursive: true });
const SECRET = 'golden-pair-secret-0123456789abcdef';
const CLI_TOKEN = 'c'.repeat(64);
// The runtime's environment as the redactor reads it: these and nothing else.
const ENV = {
  HOME: home, TZ: 'UTC', NEXTAUTH_SECRET: SECRET, API_TOKEN: 'tok-golden-value-123', DEPLOY_KEY: 'k3y-Fixture-7781',
  DB_PASSWORD: '/etc/not-a-secret', SHORT_SECRET: 'abc', MY_SECRET_PHRASE: 'has a space in it', OTHER: 'not-secret-but-long-value',
  [`${ENV_PREFIX}_WORKSPACE_DIR`]: workspace,
};
const OLD_ENV = Object.fromEntries(Object.entries(process.env).filter(([k]) => k.startsWith('OLD_RUNTIME_')));
const GEN = Object.fromEntries(Object.entries(process.env).filter(([k]) => k.startsWith('GEN_')));
for (const k of Object.keys(process.env)) delete process.env[k];
Object.assign(process.env, ENV, OLD_ENV);
globalThis.__golden = { cliToken: CLI_TOKEN, tabs: [] };

const here = path.dirname(fileURLToPath(import.meta.url));
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const { dataDir } = await import('@/lib/data-dir');
const { SignJWT } = await import('jose');
const redact = await import(`@/lib/${M}share-redact`);
const snap = await import(`@/lib/${M}share-snapshot`);
const render = await import(`@/lib/${M}share-render`);
for (const k of Object.keys(OLD_ENV)) delete process.env[k];
const DATA = dataDir();
fs.mkdirSync(DATA, { recursive: true });
fs.writeFileSync(path.join(DATA, 'cli-token'), CLI_TOKEN + '\n', { mode: 0o600 });

// The words of the previous deployment replaced by the neutral ones, in everything written out.
const neutral = (s) => s.split(root).join('$ROOT').split(OLD_PROFILE_HOMES).join(NEUTRAL.profileHomes).split(OLD_BRAND).join(NEUTRAL.brand)
  .split(`${OLD_FILE_PREFIX}-`).join(`${NEUTRAL.filePrefix}-`);

// --- random cases of the pure parts ---------------------------------------------------------------------------------
if (GEN.GEN_RANDOM) {
  const { randomCases } = await import('./random.mjs');
  // Fixed paths: a home under the temporary root would read differently once the root is written as $ROOT.
  const cases = randomCases({ n: Number(GEN.GEN_RANDOM), seed: Number(GEN.GEN_SEED ?? 1), redact, snap, render, home: '/home/agent', workspace: '/home/agent/work/acme',
    profileHomes: OLD_PROFILE_HOMES, brand: OLD_BRAND, env: ENV, cliToken: CLI_TOKEN, dropHost: GEN.GEN_DROP_HOST ?? 'example.invalid' });
  fs.writeFileSync(GEN.GEN_OUT, neutral(JSON.stringify({ root, home, workspace, cases })));
  console.log(`wrote ${cases.length} cases`);
  process.exit(0);
}

// --- fixture transcripts ----------------------------------------------------------------------------------------------
const PROJ = 'home/.claude/projects/-home-agent-work-acme';
const CODEX = 'home/.codex/sessions/2026/10/18';
const j = (rows) => rows.map((r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n';
const T = (s) => `2026-10-18T09:${String(s).padStart(2, '0')}:00.000Z`;
const SID = '7d1c2b3a-4e5f-4a6b-8c7d-9e0f1a2b3c4d';
let uuid = 0;
const line = (type, ts, message, extra = {}) => ({ type, timestamp: ts, sessionId: SID, cwd: workspace, uuid: `u${++uuid}`, message, ...extra });
const text = (s) => ({ type: 'text', text: s });
const tool = (id, name, input) => ({ type: 'tool_use', id, name, input });
const result = (id, content, isError = false) => ({ type: 'tool_result', tool_use_id: id, content, is_error: isError });
// Provider-shaped fake credentials; the golden stores them as these placeholders and the Go test expands them back.
const FAKE = { '@FAKE_ANTHROPIC_KEY@': `sk-ant-api03-${'A'.repeat(30)}`, '@FAKE_GITHUB_TOKEN@': `ghp_${'b'.repeat(36)}` };
const md = [
  '# Итоги\n\nГотово: поправил **парсер** и `config.json`. Ссылки: [docs](https://example.com/docs?a=1&b=2), <https://example.org/x>, www.example.net/path.',
  '\n\n| файл | строк |\n|:--|--:|\n| a.go | 12 |\n| b.go | 3 |\n\n```go\nfunc main() {\n\tfmt.Println("<hi>")\n}\n```\n\n- [x] тесты\n- [ ] релиз\n\n> цитата\n\n',
  `Ключ ${FAKE['@FAKE_ANTHROPIC_KEY@']} и токен ${FAKE['@FAKE_GITHUB_TOKEN@']} лежат в ${home}/work/acme/.env и /root/.ssh, а ${workspace}/src/main.go — код.`,
].join('');
const claudeRich = j([
  line('user', T(0), { role: 'user', content: '<system-reminder>internal</system-reminder>Почини парсер в acme, ключ API_TOKEN=tok-golden-value-123' }),
  line('assistant', T(1), { id: 'msg_1', role: 'assistant', model: 'claude-model-a', content: [{ type: 'thinking', thinking: 'Сначала посмотрю файлы.' }],
    usage: { input_tokens: 120, output_tokens: 40, cache_read_input_tokens: 5000, cache_creation_input_tokens: 700 } }),
  line('assistant', T(1), { id: 'msg_1', role: 'assistant', model: 'claude-model-a', content: [tool('t1', 'Bash', { command: `ls ${workspace}`, description: 'List files' })],
    usage: { input_tokens: 120, output_tokens: 40, cache_read_input_tokens: 5000, cache_creation_input_tokens: 700 } }),
  line('user', T(2), { role: 'user', content: [result('t1', 'main.go\nparser.go')] }),
  line('assistant', T(3), { id: 'msg_2', role: 'assistant', model: 'claude-model-a', content: [tool('t2', 'Edit', { file_path: `${workspace}/parser.go`, old_string: 'a := 1\nb := 2', new_string: 'a := 2\npassword = "hunter2-secret"' })],
    usage: { input_tokens: 10, output_tokens: 300 } }),
  line('user', T(4), { role: 'user', content: [result('t2', 'ok')] }),
  line('assistant', T(5), { id: 'msg_3', role: 'assistant', model: 'claude-model-a', content: [tool('t3', 'Read', { file_path: '/home/someone/notes.txt' }), tool('t4', 'Grep', { pattern: 'Bearer abcdefghijklmnopqrstuvwxyz012345' })],
    usage: { input_tokens: 10, output_tokens: 20 } }),
  line('user', T(6), { role: 'user', content: [result('t3', 'x'), result('t4', 'y', true)] }),
  line('assistant', T(7), { id: 'msg_4', role: 'assistant', model: 'claude-model-b', content: [text(md)], usage: { input_tokens: 5, output_tokens: 900 } }),
  { type: 'system', subtype: 'turn_duration', durationMs: 754000, timestamp: T(8), sessionId: SID, uuid: `u${++uuid}` },
  line('user', T(10), { role: 'user', content: [text('А теперь сделай план, вот картинка'), { type: 'image', source: { type: 'base64', media_type: 'image/png', data: 'iVBORw0KGgo=' } }] }),
  line('assistant', T(11), { id: 'msg_5', role: 'assistant', model: 'claude-model-b', content: [tool('t5', 'ExitPlanMode', { plan: '## План\n\n1. Шаг один\n2. Шаг два с https://example.com/plan' })],
    usage: { input_tokens: 3, output_tokens: 50 } }),
  line('user', T(12), { role: 'user', content: [result('t5', 'User approved')] }),
  line('assistant', T(13), { id: 'msg_6', role: 'assistant', model: 'claude-model-b', content: [text('x'.repeat(21000))], usage: { input_tokens: 1, output_tokens: 1 } }),
  line('assistant', T(14), { id: 'msg_7', role: 'assistant', model: 'claude-model-b', content: [tool('t6', 'Write', { file_path: `${workspace}/new.txt`, content: 'hello' }),
    tool('t7', 'WebSearch', { query: 'acme parser' }), tool('t8', 'mcp__docs__search', { q: 'x' }), tool('t9', 'Task', { description: 'Разведка', subagent_type: 'Explore', prompt: 'go' })],
    usage: { input_tokens: 1, output_tokens: 1 } }),
  line('user', T(15), { role: 'user', content: [result('t6', 'ok'), result('t7', 'ok'), result('t8', 'ok'), result('t9', 'ok')] }),
  { type: 'system', subtype: 'turn_duration', durationMs: 61000, timestamp: T(16), sessionId: SID, uuid: `u${++uuid}` },
  line('user', T(17), { role: 'user', content: '[Request interrupted by user]' }),
  line('assistant', T(18), { id: 'msg_8', role: 'assistant', model: 'claude-model-b', content: [text('Финал: ' + String.fromCharCode(0x202e) + 'секрет' + String.fromCharCode(0x202c) + ' скрыт, ' + '' + ['eyJhbGciOiJIUzI1NiJ9', 'eyJzdWIiOiJhY21lLXVzZXIifQ', 'c2lnbmF0dXJlLXZhbHVlLXg'].join('.') + ' and postgres://u:p4ss@db:5432/x')],
    usage: { input_tokens: 1, output_tokens: 1 } }),
  { type: 'ai-title', aiTitle: 'Парсер acme\nи план', sessionId: SID },
]);
const codex = j([
  { timestamp: T(0), type: 'session_meta', payload: { id: '55555555-5555-4555-8555-555555555555', cwd: workspace, timestamp: T(0), cli_version: '0.1.0' } },
  { timestamp: T(1), type: 'turn_context', payload: { cwd: workspace, model: 'codex-model-x', effort: 'medium' } },
  { timestamp: T(1), type: 'event_msg', payload: { type: 'user_message', message: 'List the files in /root/acme' } },
  { timestamp: T(2), type: 'response_item', payload: { type: 'reasoning', summary: [{ type: 'summary_text', text: '**Plan** look around' }] } },
  { timestamp: T(2), type: 'response_item', payload: { type: 'function_call', name: 'shell', arguments: JSON.stringify({ command: ['bash', '-lc', 'ls -la'] }), call_id: 'call_1' } },
  { timestamp: T(3), type: 'response_item', payload: { type: 'function_call_output', call_id: 'call_1', output: JSON.stringify({ output: 'a\n', metadata: { exit_code: 0 } }) } },
  { timestamp: T(3), type: 'event_msg', payload: { type: 'token_count', info: { total_token_usage: { input_tokens: 9000, cached_input_tokens: 4000, output_tokens: 300 } } } },
  { timestamp: T(4), type: 'event_msg', payload: { type: 'agent_message', message: 'There are **two** files: see `a` and https://example.com/a.' } },
  { timestamp: T(5), type: 'event_msg', payload: { type: 'task_complete', duration_ms: 42000 } },
]);
const titled = j([
  line('user', T(20), { role: 'user', content: 'hi' }),
  line('assistant', T(21), { id: 'm1', role: 'assistant', model: 'claude-model-a', content: [text('hello')], usage: { input_tokens: 1, output_tokens: 1 } }),
]);
const bigLine = JSON.stringify(line('assistant', T(30), { id: 'big', role: 'assistant', model: 'm', content: [text('y'.repeat(19000))] }));
const files = [
  { path: `${PROJ}/${SID}.jsonl`, content: claudeRich },
  { path: `${CODEX}/rollout-a.jsonl`, content: codex },
  { path: `${PROJ}/titled.jsonl`, content: titled },
  { path: `${PROJ}/big.jsonl`, repeat: { line: bigLine, count: 300 } },
  { path: `${PROJ}/recorded.jsonl`, from: '../transcript/testdata/claude/recorded-2.1.284-replies-after-idle-prompts.jsonl' },
  { path: `${CODEX}/basic.jsonl`, from: '../transcript/testdata/codex/corpus/basic.jsonl' },
  { path: `${PROJ}/null-line.jsonl`, content: '{"type":"user","timestamp":"2026-10-18T09:00:00.000Z","message":{"content":"hi"}}\nnull\n' },
];
const P = (p) => path.join(root, p);
for (const f of files) {
  fs.mkdirSync(path.dirname(P(f.path)), { recursive: true });
  const content = f.content ?? (f.repeat ? (f.repeat.line + '\n').repeat(f.repeat.count) : fs.readFileSync(path.join(here, '../..', f.from)));
  fs.writeFileSync(P(f.path), content);
}
const tab = (id, panelType, agentState, extra = {}) => ({ id, name: id, panelType, ...(agentState === undefined ? {} : { agentState }), ...extra });
globalThis.__golden.tabs = [
  tab('tab-claude', 'claude-code', { jsonlPath: P(`${PROJ}/${SID}.jsonl`), summary: 'Сводка' }),
  tab('tab-codex', 'codex-cli', { jsonlPath: P(`${CODEX}/rollout-a.jsonl`), summary: null }, { lastUserMessage: '<command-name>/x</command-name>   List   files ' }),
  tab('tab-titled', 'claude-code', { jsonlPath: P(`${PROJ}/titled.jsonl`) }, { title: 'Tab title' }),
  tab('tab-legacy', 'claude-code', undefined, { claudeJsonlPath: P(`${PROJ}/recorded.jsonl`) }),
  tab('tab-codex-basic', 'codex-cli', { jsonlPath: P(`${CODEX}/basic.jsonl`) }),
  tab('tab-big', 'claude-code', { jsonlPath: P(`${PROJ}/big.jsonl`) }),
  tab('tab-null', 'claude-code', { jsonlPath: P(`${PROJ}/null-line.jsonl`) }),
  tab('tab-shell', 'terminal'),
  tab('tab-nopath', 'claude-code', { jsonlPath: null }),
  tab('tab-missing', 'claude-code', { jsonlPath: P(`${PROJ}/missing.jsonl`) }),
];

// --- share-read tokens: each signed twice, for the old runtime (its salt and audience) and for the Go test (neutral) -
const hkdfKey = (salt) => new Uint8Array(crypto.hkdfSync('sha256', SECRET, salt, 'share-read/v1', 32));
const sign = async (claims, { salt, audience, secretKey, iatShift = 0, ttl = 60, alg = 'HS256' }) => {
  const now = Math.floor(globalThis.__now / 1000);
  let jwt = new SignJWT({ ...claims }).setProtectedHeader({ alg }).setIssuedAt(now + iatShift).setExpirationTime(now + iatShift + ttl);
  if (audience !== null) jwt = jwt.setAudience(audience);
  return jwt.sign(secretKey ?? hkdfKey(salt));
};
const tokenPair = async (claims, opts = {}) => ({
  old: await sign(claims, { salt: OLD_SALT, audience: opts.audience === undefined ? OLD_AUDIENCE : opts.audience, ...opts }),
  neutral: await sign(claims, { salt: NEUTRAL.salt, audience: opts.audience === undefined ? NEUTRAL.audience : opts.audience, ...opts }),
});

// --- the dispatcher -----------------------------------------------------------------------------------------------------
const route = (p) => ({ route: `${OLD_API}${p}`, load: () => import(`@/pages${OLD_API}${p === '/snapshots' || p === '/shares' ? `${p}/index` : p}`) });
const dispatch = createApiDispatcher([route('/snapshots'), route('/snapshots/[id]/download'), route('/snapshots/[id]/preview'), route('/shares'),
  route('/shares/[id]'), route('/share-read/[id]')]);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const S = { cookie: 'session-token=good' };
const PERSON = { cookie: 'session-token=person' };
const PW = { cookie: 'session-token=password' };
const TOK = { 'x-cli-token': CLI_TOKEN };
const JSONT = { 'content-type': 'application/json' };
const SHARE_BASE = 'https://share.example.com/share/AbCdEfGhIjKl/';
const BASE = { 'x-share-base': SHARE_BASE };
const A = '/api/platform';
const full = { tools: 'summary', thinking: true, diffs: true, economics: true };
const lean = { tools: 'none', thinking: false, diffs: false, economics: false };
const id = (n) => Buffer.alloc(32, n).toString('base64url');
const HOUR = 3600_000;
const DAY = 24 * HOUR;
// Ids the old runtime handed out, by name, as steps save them (they come from the counter, so the Go test sees the same).
const ids = {};
const tk = (sid, viewer, opts) => () => tokenPair(viewer === undefined ? { sid: sid() } : { sid: sid(), viewer }, opts);
const I = (name) => () => ids[name];
const snapshotReq = (headers, body, save) => ({ req: () => ['POST', `${A}/snapshots`, [], { ...headers, ...JSONT }, body], save });
const publish = (body, save) => ({ req: () => ['POST', `${A}/shares`, [], { ...S, ...JSONT, ...BASE }, body()], save });
const read = (sid, viewer, opts, extra = {}) => ({ req: () => ['GET', `${A}/share-read/${sid()}`, [], extra], token: tk(sid, viewer, opts) });

// Steps: a request [method, path, query, headers, body] (built when it runs), an advance of the clock, or an action on
// the files.
const steps = [
  snapshotReq({}, { tabId: 'tab-claude', options: full }),
  snapshotReq(TOK, { tabId: 'tab-claude', options: full }),
  { req: () => ['POST', `${A}/snapshots`, [], { ...S, ...JSONT }, '{bad'] },
  { req: () => ['POST', `${A}/snapshots`, [], { ...S, 'content-type': 'text/plain' }, 'x'] },
  { req: () => ['GET', `${A}/snapshots`, [], S] },
  snapshotReq(S, { tabId: 'bad id', options: full }),
  snapshotReq(S, { tabId: 'tab-claude', options: { ...full, thinking: 1 } }),
  snapshotReq(S, { tabId: 'tab-claude' }),
  snapshotReq(S, { tabId: 'tab-unknown', options: full }),
  snapshotReq(S, { tabId: 'tab-shell', options: full }),
  snapshotReq(S, { tabId: 'tab-nopath', options: full }),
  snapshotReq(S, { tabId: 'tab-missing', options: full }),
  snapshotReq(S, { tabId: 'tab-big', options: full }),
  snapshotReq(S, { tabId: 'tab-null', options: full }),
  snapshotReq(S, { tabId: 'tab-null', options: lean }, 'null'),
  snapshotReq(PERSON, { tabId: 'tab-claude', options: full }, 'claude'),
  { req: () => ['POST', '/agents' + `${A}/snapshots/`, [], { ...S, 'content-type': 'application/json; charset=utf-8' }, { tabId: 'tab-codex', options: lean }], save: 'codex' },
  snapshotReq(PW, { tabId: 'tab-titled', options: { ...full, tools: 'none' } }, 'titled'),
  snapshotReq(PERSON, { tabId: 'tab-legacy', options: full }, 'legacy'),
  snapshotReq(S, { tabId: 'tab-codex-basic', options: full }, 'basic'),
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/preview`, [], S] },
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/preview`, [], { ...S, 'if-none-match': 'ETAG' }] },
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/preview`, [], TOK] },
  { req: () => ['POST', `${A}/snapshots/${ids.claude}/preview`, [], S] },
  { req: () => ['GET', `${A}/snapshots/${id(99)}/preview`, [], S] },
  { req: () => ['GET', `${A}/snapshots/short/preview`, [], S] },
  ...['claude', 'codex', 'titled', 'legacy', 'basic', 'null'].flatMap((name) => [
    { req: () => ['GET', `${A}/snapshots/${ids[name]}/preview`, [], S] },
    { req: () => ['GET', `${A}/snapshots/${ids[name]}/download`, [['format', 'html']], S] },
    { req: () => ['GET', `${A}/snapshots/${ids[name]}/download`, [['format', 'md']], S] },
  ]),
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/download`, [['format', 'pdf']], S] },
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/download`, [['format', 'md'], ['format', 'md']], S] },
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/download`, [['format', 'md']], TOK] },
  { req: () => ['GET', `${A}/shares`, [], S] },
  { req: () => ['GET', `${A}/shares`, [], TOK] },
  { req: () => ['POST', `${A}/shares`, [], { ...S, ...JSONT }, { draftId: ids.claude, expiresInDays: 7 }] },
  { req: () => ['POST', `${A}/shares`, [], { ...S, ...JSONT, 'x-share-base': 'http://evil.example/share/AbCdEfGhIjKl/' }, { draftId: ids.claude, expiresInDays: 7 }] },
  { req: () => ['POST', `${A}/shares`, [], { ...S, ...BASE, 'content-type': 'text/plain' }, 'x'] },
  publish(() => ({ draftId: 'nope', expiresInDays: 7 })),
  publish(() => ({ draftId: ids.claude, expiresInDays: 8 })),
  publish(() => ({ draftId: ids.claude, expiresInDays: '7' })),
  publish(() => ({ draftId: ids.claude, expiresInDays: 7, replaceId: 5 })),
  publish(() => ({ draftId: id(99), expiresInDays: 7 })),
  publish(() => ({ draftId: ids.claude, expiresInDays: 7, replaceId: null }), 'share1'),
  { req: () => ['GET', `${A}/snapshots/${ids.claude}/preview`, [], S] },
  { advance: 60_000 },
  publish(() => ({ draftId: ids.codex, expiresInDays: 90 }), 'share2'),
  publish(() => ({ draftId: ids.legacy, expiresInDays: 30 }), 'share3'),
  { req: () => ['GET', `${A}/shares`, [], { ...S, ...BASE }] },
  { req: () => ['GET', `${A}/shares`, [['tabId', 'tab-claude']], S] },
  { req: () => ['GET', `${A}/shares`, [['tabId', 'bad id']], S] },
  { req: () => ['GET', `${A}/shares`, [['tabId', 'a'], ['tabId', 'b']], S] },
  { req: () => ['PATCH', `${A}/shares/${ids.share1}`, [], { ...S, ...JSONT }, { expiresInDays: 30 }] },
  { req: () => ['PATCH', `${A}/shares/${ids.share1}`, [], { ...S, 'content-type': 'text/plain' }, 'x'] },
  { req: () => ['PATCH', `${A}/shares/${ids.share1}`, [], { ...S, ...JSONT }, { expiresInDays: 8 }] },
  { req: () => ['PUT', `${A}/shares/${ids.share1}`, [], { ...S, ...JSONT }, {}] },
  { req: () => ['PATCH', `${A}/shares/${id(98)}`, [], { ...S, ...JSONT }, { expiresInDays: 30 }] },
  { req: () => ['PATCH', `${A}/shares/bad`, [], { ...S, ...JSONT }, { expiresInDays: 30 }] },
  { req: () => ['PATCH', `${A}/shares/${ids.share1}`, [], { ...TOK, ...JSONT }, { expiresInDays: 30 }] },
  read(I('share1'), 'bob@example.com'),
  read(I('share1'), 'carol@example.com'),
  read(I('share1'), 'bob@example.com'),
  read(I('share1'), 'bob@example.com', {}, { 'if-none-match': 'ETAG' }),
  read(I('share2'), 'bob@example.com'),
  read(I('share3'), 'erin@example.com'),
  read(I('share1'), 'bob@example.com', { ttl: 300 }),
  read(I('share1'), 'bob@example.com', { iatShift: -200 }),
  read(I('share1'), 'bob@example.com', { iatShift: 30 }),
  read(I('share1'), 'bob@example.com', { audience: 'other' }),
  read(I('share1'), 'bob@example.com', { audience: null }),
  read(I('share1'), ''),
  read(I('share1'), 'v'.repeat(257)),
  read(I('share1'), undefined),
  read(I('share1'), 'bob@example.com', { secretKey: new TextEncoder().encode('wrong-key-wrong-key-wrong-key-00') }),
  read(I('share2'), 'bob@example.com', {}, {}),
  { req: () => ['GET', `${A}/share-read/${ids.share1}`, [], {}], token: () => ({ old: 'garbage', neutral: 'garbage' }) },
  { req: () => ['GET', `${A}/share-read/${ids.share1}`, [], {}] },
  { req: () => ['POST', `${A}/share-read/${ids.share1}`, [], {}], token: tk(I('share1'), 'bob@example.com') },
  { req: () => ['GET', `${A}/share-read/${ids.share1}`, [], {}], token: tk(I('share2'), 'bob@example.com') },
  read(() => id(97), 'bob@example.com'),
  read(() => 'short', 'bob@example.com'),
  snapshotReq(PERSON, { tabId: 'tab-claude', options: { ...full, diffs: false } }, 'claude2'),
  publish(() => ({ draftId: ids.claude2, expiresInDays: 30, replaceId: ids.share1 }), 'share1b'),
  snapshotReq(S, { tabId: 'tab-codex', options: full }, 'codex2'),
  publish(() => ({ draftId: ids.codex2, expiresInDays: 30, replaceId: ids.share1 })),
  publish(() => ({ draftId: ids.codex2, expiresInDays: 30, replaceId: id(96) })),
  read(I('share1'), 'dave@example.com'),
  { req: () => ['DELETE', `${A}/shares/${ids.share3}`, [], S] },
  { req: () => ['DELETE', `${A}/shares/${ids.share3}`, [], S] },
  { req: () => ['DELETE', `${A}/shares/${id(95)}`, [], S] },
  read(I('share3'), 'bob@example.com'),
  { req: () => ['GET', `${A}/shares`, [], S] },
  { touch: () => `shares/drafts/${ids.basic}.json`, mtime: () => globalThis.__now - 2 * HOUR },
  snapshotReq(S, { tabId: 'tab-titled', options: full }, 'titled2'),
  { advance: 2 * HOUR },
  { req: () => ['GET', `${A}/snapshots/${ids.titled}/preview`, [], S] },
  { advance: 31 * DAY },
  read(I('share1'), 'bob@example.com'),
  read(I('share2'), 'bob@example.com'),
  { req: () => ['PATCH', `${A}/shares/${ids.share1}`, [], { ...S, ...JSONT }, { expiresInDays: 90 }] },
  { req: () => ['PATCH', `${A}/shares/${ids.share2}`, [], { ...S, ...JSONT }, { expiresInDays: 90 }] },
  { req: () => ['GET', `${A}/shares`, [], S] },
  { advance: 31 * DAY },
  { req: () => ['GET', `${A}/shares`, [], S] },
  { req: () => ['DELETE', `${A}/shares/${ids.share1}`, [], S] },
  { advance: 40 * DAY },
  read(I('share2'), 'bob@example.com'),
  { req: () => ['GET', `${A}/shares`, [['tabId', 'tab-codex']], S] },
  { req: () => ['GET', `${A}/shares`, [], S] },
];

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length', 'content-disposition', 'content-security-policy',
  'x-content-type-options', 'x-frame-options', 'referrer-policy'];
const toOld = (headers) => {
  const out = {};
  for (const [k, v] of Object.entries(headers)) {
    if (NEUTRAL.headers.includes(k)) out[OLD_HEADERS[NEUTRAL.headers.indexOf(k)]] = v;
    else if (k === NEUTRAL.base) out[OLD_BASE_HEADER] = v;
    else out[k] = v;
  }
  return out;
};
const shareFiles = () => {
  const out = {};
  const walk = (dir) => {
    for (const name of fs.existsSync(dir) ? fs.readdirSync(dir).sort() : []) {
      const file = path.join(dir, name);
      if (fs.statSync(file).isDirectory()) walk(file);
      else out[path.relative(DATA, file)] = neutral(fs.readFileSync(file, 'utf8'));
    }
  };
  walk(path.join(DATA, 'shares'));
  return out;
};
const out = [];
let lastEtag = '';
let prevFiles = {};
// The share files a step changed: written (new or different content) and removed.
const filesDelta = () => {
  const now = shareFiles();
  const written = {};
  for (const [k, v] of Object.entries(now)) if (prevFiles[k] !== v) written[k] = v;
  const removed = Object.keys(prevFiles).filter((k) => !(k in now));
  prevFiles = now;
  return { written, removed };
};
for (const step of steps) {
  if (step.advance) {
    globalThis.__now += step.advance;
    out.push(step);
    continue;
  }
  if (step.touch) {
    const rel = step.touch();
    const mtime = step.mtime();
    fs.utimesSync(path.join(DATA, rel), mtime / 1000, mtime / 1000);
    out.push({ touch: rel, mtime });
    continue;
  }
  const [method, urlPath, query, headers, body] = step.req();
  const token = step.token ? await step.token() : undefined;
  const h = toOld({ ...headers });
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  if (token) h[OLD_TOKEN_HEADER] = token.old;
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
  const url = urlPath.replace(A, OLD_API) + (qs ? `?${qs}` : '');
  const raw = body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body);
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: h }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    if (raw !== undefined) req.write(raw);
    req.end();
  });
  if (res.headers.etag) lastEtag = res.headers.etag;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = neutral(String(res.headers[k]));
  // Neutral words change the bytes the tag was taken over; the test checks the tag against the neutral bytes.
  if (kept.etag && neutral(res.text) !== res.text) kept.etag = 'RECOMPUTE';
  if (res.status === 304) kept.etag = 'IF-NONE-MATCH'; // the tag the request held
  if (kept['content-length'] && neutral(res.text) !== res.text) kept['content-length'] = String(Buffer.byteLength(neutral(res.text)));
  if (step.save && res.status < 300) {
    const v = JSON.parse(res.text);
    ids[step.save] = v.draft?.id ?? v.share?.id;
  }
  out.push({ now: globalThis.__now, method, path: urlPath, query, headers, body: raw, token: token?.neutral, status: res.status,
    answerHeaders: kept, answer: neutral(res.text), files: filesDelta() });
}
server.close();

const golden = {
  secret: SECRET, cliToken: CLI_TOKEN, env: Object.entries(ENV).map(([k, v]) => (k === `${ENV_PREFIX}_WORKSPACE_DIR` ? 'ACME_WORKSPACE_DIR=$WORKSPACE' : `${k}=${neutral(v)}`)),
  shareBase: SHARE_BASE, neutral: NEUTRAL,
  files: files.map((f) => ({ path: f.path, content: f.content === undefined ? undefined : neutral(f.content), repeat: f.repeat && { line: neutral(f.repeat.line), count: f.repeat.count }, from: f.from })),
  tabs: JSON.parse(neutral(JSON.stringify(globalThis.__golden.tabs))),
  steps: out,
};
// Nothing a secret scanner looks for is committed: the fake credentials become their placeholders (their characters
// never need JSON escaping, so the raw text holds them as they are) and other token shapes get one letter escaped.
const unscannable = (json) => Object.entries(FAKE).reduce((s, [ph, v]) => s.split(v).join(ph), json).replace(/gh([pousr])_/g, (_, c) => `gh\\u00${c.charCodeAt(0).toString(16)}_`).replace(/sk-ant-/g, 's\\u006b-ant-').replace(/eyJ/g, 'e\\u0079J');
fs.writeFileSync(path.join(here, '..', 'golden.json'), unscannable(JSON.stringify(golden, null, 1)) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`wrote ${out.length} steps`);
