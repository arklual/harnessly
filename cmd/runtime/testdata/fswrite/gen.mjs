// Sends a fixed list of requests to the previous runtime's own API dispatcher and its disk-writing handlers (uploads,
// the uploads clean-up, an editor save, a repository sync, a process stop), on fixture folders and repositories, and
// writes what each answered and the files left to ../fswrite-golden.json. The Go test lays out the same, replays the
// requests on its routes and must match.
//
// The previous runtime's product names are not spelled here: the caller passes them, and the output is neutral.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> OLD_RUNTIME_PLATFORM_API=<its platform prefix> \
//   OLD_UPLOAD_HEADER_PREFIXES=<current,legacy upload header prefixes> OLD_DATA_DIR_VAR=<its data folder variable> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import { syncBuiltinESMExports } from 'node:module';
import os from 'node:os';
import path from 'node:path';

const NEEDED = ['OLD_RUNTIME_TOKEN_HEADERS', 'OLD_RUNTIME_PLATFORM_API', 'OLD_UPLOAD_HEADER_PREFIXES', 'OLD_DATA_DIR_VAR'];
for (const k of NEEDED) if (!process.env[k]) throw new Error(`${k} is required`);
const OLD_TOKENS = process.env.OLD_RUNTIME_TOKEN_HEADERS.split(',');
const OLD_PREFIXES = process.env.OLD_UPLOAD_HEADER_PREFIXES.split(',');
const OLD_API = process.env.OLD_RUNTIME_PLATFORM_API;
const NEUTRAL_TOKENS = ['x-cli-token', 'x-old-cli-token'];
const NEUTRAL_PREFIXES = ['x-acme-', 'x-old-'];
const here = path.dirname(new URL(import.meta.url).pathname);

const FIXED = Date.parse('2026-10-18T09:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
// Stored names carry 4 random bytes: here a counter, as in the Go test.
let draws = 0;
crypto.randomBytes = (n) => { draws += 1; const b = Buffer.alloc(n); b.writeUInt32BE(draws, n - 4); return b; };
syncBuiltinESMExports();

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'fswrite-golden-')));
const home = path.join(root, 'home');
const data = path.join(root, 'data');
for (const d of [home, data]) fs.mkdirSync(d, { recursive: true });
process.env.HOME = home;
process.env.GIT_CONFIG_NOSYSTEM = '1';
process.env[process.env.OLD_DATA_DIR_VAR] = data;
process.chdir(root);
const TOKEN = 'c'.repeat(64);
fs.writeFileSync(path.join(data, 'cli-token'), TOKEN, { mode: 0o600 });

// --- fixtures -----------------------------------------------------------------------------------------------------------
// The editor's project.
const proj = path.join(root, 'proj');
const fixtures = [
  { path: 'proj/a.txt', text: 'hello\n' },
  { path: 'proj/b.txt', text: 'second\n' },
  { path: 'proj/c.txt', text: 'third\n' },
  { path: 'proj/bin.dat', base64: Buffer.from('a\0b').toString('base64') },
  { path: 'proj/latin.txt', base64: Buffer.from([0x63, 0x61, 0x66, 0xe9, 0x0a]).toString('base64') },
  { path: 'proj/big.txt', repeat: 'x', count: 1_000_001 },
  { path: 'proj/sub/d.txt', text: 'deep\n' },
  { path: 'proj/link-in', link: 'a.txt' },
  { path: 'proj/link-out', link: '../outside/secret.txt' },
  { path: 'outside/secret.txt', text: 'secret\n' },
  // Uploads the clean-up finds: <ws>/<tab>/<file>, old and new, empty files, files and folders where none belong.
  { path: 'data/uploads/ws1/tab1/old.png', text: 'old', age: 90000 },
  { path: 'data/uploads/ws1/tab1/new.png', text: 'new', age: 60 },
  { path: 'data/uploads/ws1/tab1/old-empty.png', text: '', age: 90000 },
  { path: 'data/uploads/ws1/tab2/old.pdf', text: 'pdf-old', age: 86401 },
  { path: 'data/uploads/ws1/tab2/edge.pdf', text: 'edge', age: 86399 },
  { path: 'data/uploads/ws1/stray.txt', text: 'stray', age: 90000 },
  { path: 'data/uploads/ws2/tab/sub/deep.png', text: 'deep', age: 90000 },
  { path: 'data/uploads/ws3/tab/x.png', text: 'x', age: 90000 },
  { path: 'data/uploads/top.txt', text: 'top', age: 90000 },
];
const lay = (base) => {
  for (const f of fixtures) {
    const p = path.join(base, f.path);
    fs.mkdirSync(path.dirname(p), { recursive: true });
    if (f.link) fs.symlinkSync(f.link, p);
    else fs.writeFileSync(p, f.base64 !== undefined ? Buffer.from(f.base64, 'base64') : f.repeat ? f.repeat.repeat(f.count) : f.text);
    if (f.age !== undefined) fs.utimesSync(p, (FIXED - f.age * 1000) / 1000, (FIXED - f.age * 1000) / 1000);
  }
};
lay(root);

// The repositories a sync runs in: the same script in the Go test gives the same commits.
const GIT_SETUP = `set -e
export GIT_AUTHOR_NAME=A GIT_AUTHOR_EMAIL=a@example.com GIT_COMMITTER_NAME=A GIT_COMMITTER_EMAIL=a@example.com
export GIT_AUTHOR_DATE=2026-10-18T09:00:00Z GIT_COMMITTER_DATE=2026-10-18T09:00:00Z
g() { git -c init.defaultBranch=main -c advice.detachedHead=false "$@"; }
cd "$ROOT"
g init -q --bare remote.git
g clone -q remote.git seed 2>/dev/null
(cd seed && echo one > f && g add f && g commit -qm one && g push -q origin main)
for n in uptodate behind ahead diverged local fetchfail; do g clone -q remote.git $n; done
g init -q noup
(cd noup && echo x > f && g add f && g commit -qm x)
mkdir norepo
(cd seed && echo two >> f && g commit -qam two && g push -q origin main)
(cd uptodate && g pull -q --ff-only)
(cd ahead && g pull -q --ff-only && echo three > g && g add g && g commit -qm three)
(cd diverged && echo local > h && g add h && g commit -qm local)
(cd local && echo dirty > f)
(cd fetchfail && g remote set-url origin "$ROOT/missing.git")
`;
execFileSync('sh', ['-c', GIT_SETUP], { env: { ...process.env, ROOT: root }, stdio: 'ignore' });
const gitVersion = execFileSync('git', ['--version'], { encoding: 'utf8' }).trim();

const syncs = ['norepo', 'noup', 'uptodate', 'behind', 'ahead', 'diverged', 'local', 'fetchfail'];
globalThis.__golden = { sessions: Object.fromEntries([['edit-1', proj], ['edit-gone', path.join(root, 'nowhere')], ...syncs.map((n) => [`sync-${n}`, path.join(root, n)])]) };

const { readWorkspaceFile } = await import('@/lib/workspace-files');
const revision = async (p) => (await readWorkspaceFile(proj, p)).revision;
const revA = await revision('a.txt');
const revB = await revision('b.txt');
const revC = await revision('c.txt');
const revD = await revision('sub/d.txt');

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const routes = [
  ['/api/upload-file', 'upload-file'], ['/api/upload-image', 'upload-image'], ['/api/uploads/cleanup', 'uploads/cleanup'],
  ['/api/editor/file', 'editor/file'], ['/api/git/sync', 'git/sync'], [`${OLD_API}/machine/stop`, `${OLD_API.slice(5)}/machine/stop`],
].map(([route, file]) => ({ route, load: () => import(`@/pages/api/${file}`) }));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

// --- requests -----------------------------------------------------------------------------------------------------------
const ses = { cookie: 'session-token=good' };
const tok = { 'x-cli-token': TOKEN };
const json = { 'content-type': 'application/json' };
const png = { 'content-type': 'image/png' };
const up = (name, ws, tab) => ({ ...(name !== undefined ? { 'x-acme-filename': name } : {}), ...(ws !== undefined ? { 'x-acme-ws-id': ws } : {}), ...(tab !== undefined ? { 'x-acme-tab-id': tab } : {}) });
const edit = (p, extra = []) => [['session', 'edit-1'], ['path', p], ...extra];
const put = (content, rev) => JSON.stringify({ content, revision: rev });
const STOP = '/api/platform/machine/stop';
const requests = [
  // uploads
  ['POST', '/api/upload-file', [], { ...up('notes.txt', 'ws-a', 'tab-1') }, 'file body'],
  ['POST', '/api/upload-file', [], { ...tok, ...up('notes.txt', 'ws-a', 'tab-1') }, 'file body'],
  ['POST', '/api/upload-file', [], { ...ses, ...up('notes.txt', 'ws-a', 'tab-1') }, 'file body'],
  ['POST', '/api/upload-file', [], { ...ses, ...up('notes.txt', 'ws-a', 'tab-1') }, ''],
  ...[
    'archive.tar.gz', '.bashrc', 'a.', '..', '...', 'dir%2Fsub%2Fname.md', 'with space %26 (1).PNG', '%D0%9E%D1%82%D1%87%D1%91%D1%82.pdf',
    '%F0%9F%98%80smile.txt', 'café.txt', 'x'.repeat(60) + '.verylongextension_with-chars', 'name.e-x_t', '%E0%A4%A', '%ZZ', '%C0%80',
    '%ED%A0%80', '', 'noext', 'a/b/', '/abs/file.js', '%2E%2E%2Fescape', 'tab\tname.txt',
  ].map((n, i) => ['POST', '/api/upload-file', [], { ...ses, ...up(n, 'ws-a', `tab-${i}`) }, `body ${i}`]),
  ['POST', '/api/upload-file', [], { ...ses, ...up('f.txt', '../../etc', 'ta b/..') }, 'x'],
  ['POST', '/api/upload-file', [], { ...ses, ...up('f.txt', '', '') }, 'x'],
  ['POST', '/api/upload-file', [], { ...ses, ...up('f.txt', '\u00ff' + 'w'.repeat(70)) }, 'x'],
  ['POST', '/api/upload-file', [], { ...ses, 'x-old-filename': 'old.txt', 'x-old-ws-id': 'old-ws', 'x-old-tab-id': 'old-tab' }, 'x'],
  ['POST', '/api/upload-file', [], { ...ses, ...up('new.txt', 'new-ws'), 'x-old-filename': 'old.txt', 'x-old-ws-id': 'old-ws', 'x-old-tab-id': 'old-tab' }, 'x'],
  ['POST', '/agents/api/upload-file/', [], { ...ses, ...up('agents.txt', 'ws-b', 'tab-b') }, 'x'],
  ['POST', '/api/upload-image', [], { ...ses, ...png, ...up('shot.png', 'ws-a', 'tab-1') }, 'png-bytes'],
  ['POST', '/api/upload-image', [], { ...ses, 'content-type': 'image/jpeg; charset=binary', ...up('photo.HEIC', 'ws-a', 'tab-1') }, 'jpg'],
  ['POST', '/api/upload-image', [], { ...ses, 'content-type': 'image/webp', ...up('', 'ws-a') }, 'webp'],
  ['POST', '/api/upload-image', [], { ...ses, 'content-type': 'image/gif', ...up('%F0%9F%98%80') }, 'gif'],
  ['POST', '/api/upload-image', [], { ...ses, 'content-type': 'image/PNG', ...up('x.png') }, 'x'],
  ['POST', '/api/upload-image', [], { ...ses, 'content-type': 'image/svg+xml', ...up('x.svg') }, 'x'],
  ['POST', '/api/upload-image', [], { ...ses, ...up('x.png') }, 'x'],
  ['POST', '/api/upload-image', [], { ...ses, ...png, ...up('x.png') }, ''],
  ['POST', '/api/upload-image', [], { ...ses, ...png, ...up('%E0%A4%A') }, 'x'],
  ['POST', '/api/upload-image', [], { ...png, ...up('x.png') }, 'x'],
  // clean-up
  ['POST', '/api/uploads/cleanup', [], { ...json }, '{}'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, ...json }, '{bad'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, ...json }, 'x'.repeat(1024 * 1024 + 1)],
  ['POST', '/api/uploads/cleanup', [], { ...ses, ...json }, '{"mode":"expired"}', 'tree'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, ...json }, 'null', 'tree'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, 'content-type': 'application/x-www-form-urlencoded' }, 'mode=all&mode=all', 'tree'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, 'content-type': 'text/plain' }, '{"mode":"all"}', 'tree'],
  ['POST', '/api/uploads/cleanup', [], { ...tok, 'content-type': 'application/x-www-form-urlencoded' }, 'mode=all', 'tree'],
  ['POST', '/api/uploads/cleanup', [], { ...ses, ...json }, '{"mode":"all"}', 'tree'],
  // the editor
  ['PUT', '/api/editor/file', edit('a.txt'), { ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...tok, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json, 'sec-fetch-site': 'cross-site' }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, '{bad'],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, 'x'.repeat(6 * 1024 * 1024 + 1)],
  ['PUT', '/api/editor/file', [['session', 'bad name'], ['path', 'a.txt']], { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', [['session', 'edit-1'], ['session', 'edit-1'], ['path', 'a.txt']], { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', [['session', 'missing'], ['path', 'a.txt']], { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', [['session', 'edit-1']], { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit(''), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, 'content-type': 'Application/JSON' }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, 'content-type': 'application/jsonish' }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, JSON.stringify({ content: 'x' })],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, JSON.stringify({ content: 'x', revision: revA.toUpperCase() })],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, JSON.stringify({ Content: 'x', revision: revA })],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, '[1]'],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, put('x', 'f'.repeat(64))],
  ['PUT', '/api/editor/file', edit('/etc/hostname'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('../outside/secret.txt'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('link-out'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('missing.txt'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('sub'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('bin.dat'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('latin.txt'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('big.txt'), { ...ses, ...json }, put('x', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, put('nul\u0000here', revA)],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, '{"content":"lone \\ud800 here","revision":"' + revA + '"}'],
  ['PUT', '/api/editor/file', edit('a.txt'), { ...ses, ...json }, '{"content":"pair \\ud83d\\ude00 and \\u00e9","revision":"' + revA + '","x":"\\udc00"}'],
  ['PUT', '/api/editor/file', edit('b.txt'), { ...ses, ...json }, put('y'.repeat(1_000_001), revB)],
  ['PUT', '/api/editor/file', edit('b.txt'), { ...ses, ...json }, '{"content":"first","content":"second\\n","revision":"' + revB + '"}'],
  ['PUT', '/api/editor/file', edit('b.txt'), { ...ses, ...json }, put('stale', revB)],
  ['PUT', '/api/editor/file', edit('c.txt'), { ...ses, ...json, 'sec-fetch-site': 'same-site' }, '﻿' + put('Привет\r\n', revC)],
  ['PUT', '/api/editor/file', edit('sub/d.txt'), { ...ses, 'content-type': 'application/json; charset=utf-8' }, put('', revD)],
  ['PUT', '/agents/api/editor/file/', edit('link-in'), { ...ses, ...json }, put('via link\n', revA)],
  ['PUT', '/api/editor/file', [['session', 'edit-gone'], ['path', 'a.txt']], { ...ses, ...json }, put('x', revA)],
  // syncs
  ['POST', '/api/git/sync', [['session', 'sync-behind']], {}, ''],
  ['POST', '/api/git/sync', [], { ...ses }, ''],
  ['POST', '/api/git/sync', [['session', '']], { ...ses }, ''],
  ['POST', '/api/git/sync', [['session', 'missing']], { ...ses }, ''],
  ['POST', '/api/git/sync', [['session', 'sync-behind']], { ...ses, ...json }, '{bad'],
  ...syncs.map((n) => ['POST', '/api/git/sync', [['session', `sync-${n}`]], { ...tok }, '']),
  // stop, as far as it goes without a process of this machine
  ['POST', STOP, [], { ...tok, ...json }, '{"pid":4194301,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, ...json, 'sec-fetch-site': 'same-site' }, '{"pid":4194301,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, ...json, 'sec-fetch-site': 'cross-site' }, '{"pid":4194301,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, 'content-type': 'text/plain' }, '{"pid":4194301,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, ...json }, '{bad'],
  ['POST', STOP, [], { ...ses, ...json }, '[]'],
  ['POST', STOP, [], { ...ses, ...json }, '{"pid":1,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, ...json, 'sec-fetch-site': 'none' }, '{"pid":4194301,"starttime":"1"}'],
  ['POST', STOP, [], { ...ses, 'content-type': 'Application/JSON; charset=utf-8' }, '{"pid":4194301,"starttime":4}'],
  ['POST', STOP, [], { ...ses, ...json }, 'x'.repeat(1024 * 1024 + 1)],
];

// --- run ------------------------------------------------------------------------------------------------------------------
const snapshot = (dir) => {
  const out = [];
  const walk = (d) => {
    let names;
    try { names = fs.readdirSync(d).sort(); } catch { return; }
    for (const n of names) {
      const p = path.join(d, n);
      const st = fs.lstatSync(p);
      const rel = path.relative(root, p);
      if (st.isSymbolicLink()) out.push({ path: rel, link: fs.readlinkSync(p) });
      else if (st.isDirectory()) { out.push({ path: rel, dir: true }); walk(p); }
      else out.push({ path: rel, mode: (st.mode & 0o777).toString(8), base64: st.size > 4096 ? null : fs.readFileSync(p).toString('base64'), size: st.size });
    }
  };
  walk(dir);
  return out;
};
const toOld = (headers) => Object.fromEntries(Object.entries(headers).map(([k, v]) => {
  if (NEUTRAL_TOKENS.includes(k)) return [OLD_TOKENS[NEUTRAL_TOKENS.indexOf(k)], v];
  for (const [i, p] of NEUTRAL_PREFIXES.entries()) if (k.startsWith(p)) return [OLD_PREFIXES[i] + k.slice(p.length), v];
  return [k, v];
}));
// A revision hashes the project's real path: the Go test computes its own for {{rev:<file>}} and compares answers
// with every revision written {{rev}}.
const revs = [[revA, 'a.txt'], [revB, 'b.txt'], [revC, 'c.txt'], [revD, 'sub/d.txt']];
const scrub = (s) => s.split(root).join('$ROOT').replace(/"revision":"[0-9a-f]{64}"/g, '"revision":"{{rev}}"');
const neutralBody = (b) => revs.reduce((acc, [rev, file]) => acc.split(rev).join(`{{rev:${file}}}`), b);
// A long body is one long run of a character between a head and a tail.
const packBody = (b) => {
  if (b.length <= 4096) return b;
  let best = [0, 0];
  for (let i = 0; i < b.length;) {
    let j = i;
    while (j < b.length && b[j] === b[i]) j += 1;
    if (j - i > best[1] - best[0]) best = [i, j];
    i = j;
  }
  return { head: b.slice(0, best[0]), repeat: b[best[0]], count: best[1] - best[0], tail: b.slice(best[1]) };
};
const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length'];
const out = [];
for (const [method, urlPath, query, headers, body, after] of requests) {
  const qs = query.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
  const url = urlPath.replace('/api/platform', OLD_API) + (qs ? `?${qs}` : '');
  const payload = Buffer.from(body, 'utf8');
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + url, { method, headers: { ...toOld(headers), 'content-length': payload.length } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', (err) => resolve({ status: 0, headers: {}, text: String(err.code) }));
    req.end(payload);
  });
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  const entry = { method, path: urlPath, query, headers, body: packBody(neutralBody(body)),
    status: res.status, answerHeaders: kept, answer: scrub(res.text) };
  if (after === 'tree') entry.uploads = snapshot(path.join(data, 'uploads'));
  out.push(entry);
}
server.close();
const trees = { uploads: snapshot(path.join(data, 'uploads')), proj: snapshot(proj) };
for (const n of syncs.filter((x) => x !== 'norepo')) trees[n] = execFileSync('git', ['-C', path.join(root, n), 'log', '--all', '--format=%H %D'], { encoding: 'utf8' });
fs.writeFileSync(path.join(here, '..', 'fswrite-golden.json'), JSON.stringify({ now: FIXED, token: TOKEN, gitVersion, gitSetup: GIT_SETUP, fixtures, requests: out, trees }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests → ${path.join(here, '..', 'fswrite-golden.json')}`);
