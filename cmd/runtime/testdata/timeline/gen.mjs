// Runs scripted cases through the previous runtime's own modules and handlers and writes what they answered, for the
// Go tests to replay on the same inputs:
//   - ../../../../internal/checkpoints/testdata/golden.json: the checkpoint store (snapshots, list, diff, rewind with its
//     races, trimming, pruning) step by step, each step's result and the shadow repository and working folder after;
//   - ../checkpoints-golden.json: requests to the checkpoints route through the old API dispatcher;
//   - ../changes-golden.json: requests to the changes route (the POST of a conversation's changes, the GET of a commit).
//
// Product names are not spelled here: the old runtime's cli-token header, its platform prefix and the namespace of its
// checkpoint refs come from the caller and are written neutrally (x-cli-token, /api/platform, refs/NS/).
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> \
//   OLD_RUNTIME_PLATFORM_API=<its platform prefix> OLD_RUNTIME_CHECKPOINT_NAMESPACE=<its ref namespace> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// GOLDEN_RANDOM=<n> GOLDEN_RANDOM_MODULE=<module exporting randomScripts(n, t0)> GOLDEN_OUT=<file> instead runs n
// randomized store scripts and writes them with their results to GOLDEN_OUT, for a one-off larger comparison.
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const need = (k) => {
  const v = process.env[k];
  if (!v) throw new Error(`${k} is required`);
  return v;
};
const OLD_HEADER = need('OLD_RUNTIME_TOKEN_HEADER');
const OLD_PREFIX = need('OLD_RUNTIME_PLATFORM_API');
const OLD_NS = need('OLD_RUNTIME_CHECKPOINT_NAMESPACE');
const PREFIX = '/api/platform';
const TOKEN_HEADER = 'x-cli-token';

const T0 = 1760000000000;
globalThis.__now = T0;
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(globalThis.__now); }
  static now() { return globalThis.__now; }
};

const ROOT = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'checkpoints-golden-')));
const HOME = path.join(ROOT, 'home');
const DATA = path.join(ROOT, 'data');
for (const d of [HOME, DATA, path.join(ROOT, 'xdg')]) fs.mkdirSync(d, { recursive: true });
process.env.HOME = HOME;
process.env.XDG_CONFIG_HOME = path.join(ROOT, 'xdg');
process.env.GOLDEN_DATA_DIR = DATA;
globalThis.__golden = { tabs: [], cwds: {} };

// The old modules are named after the product, like its platform prefix.
const PRODUCT = OLD_PREFIX.split('/').pop();
const lib = await import(`@/lib/${PRODUCT}-checkpoints`);
const { snapshotTurnEnd } = await import(`@/lib/${PRODUCT}-checkpoint-turn`);
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');

// --- shared helpers: the same actions and dumps the Go tests implement ---

const nameOf = (a) => (a.pathB64 ? Buffer.concat([Buffer.from(ROOT + '/'), Buffer.from(a.pathB64, 'base64')]) : path.join(ROOT, a.path));
const gitEnv = (extra = {}) => {
  const env = {};
  for (const [k, v] of Object.entries(process.env)) if (!k.startsWith('GIT_')) env[k] = v;
  return { ...env, ...extra };
};
const run = (args, cwd, extra) => {
  const r = spawnSync('git', args, { cwd, env: gitEnv(extra), maxBuffer: 64 << 20 });
  if (r.status !== 0) throw new Error(`git ${args.join(' ')}: ${r.error ?? r.stderr}`);
  return r.stdout.toString('utf8');
};

// A file action that fails (a folder where a file should go, a link in the way) is skipped from where it failed.
const applyAction = (a) => {
  const p = nameOf(a);
  if (a.git) {
    const date = a.date ? `${a.date} +0000` : undefined;
    run(a.git, p, {
      GIT_AUTHOR_NAME: 'Ann Example', GIT_AUTHOR_EMAIL: 'ann@example.com', GIT_COMMITTER_NAME: 'Ann Example',
      GIT_COMMITTER_EMAIL: 'ann@example.com', ...(date ? { GIT_AUTHOR_DATE: date, GIT_COMMITTER_DATE: date } : {}),
    });
    return;
  }
  try {
    if (a.write !== undefined) {
      fs.mkdirSync(path.dirname(p.toString()), { recursive: true });
      const data = a.b64 !== undefined ? Buffer.from(a.b64, 'base64') : Buffer.from(a.repeat !== undefined ? a.repeat.repeat(a.count) : a.write);
      fs.rmSync(p, { force: true, recursive: true });
      fs.writeFileSync(p, data);
      if (a.mode !== undefined) fs.chmodSync(p, a.mode);
    } else if (a.link !== undefined) {
      fs.mkdirSync(path.dirname(p.toString()), { recursive: true });
      fs.rmSync(p, { force: true, recursive: true });
      fs.symlinkSync(a.link, p);
    } else if (a.rm) fs.rmSync(p, { force: true, recursive: true });
    else if (a.mkdir) fs.mkdirSync(p, { recursive: true });
    else if (a.chmod !== undefined) fs.chmodSync(p, a.chmod);
    else if (a.mtime !== undefined) fs.utimesSync(p, new RealDate(a.mtime), new RealDate(a.mtime));
    else throw new Error(`unknown action ${JSON.stringify(a)}`);
  } catch (err) {
    if (String(err.message).startsWith('unknown action')) throw err;
  }
};
const applyActions = (list) => { for (const a of list ?? []) applyAction(a); };

const hashes = new Map();
const noteRoot = (cwd) => {
  const real = fs.realpathSync(path.join(ROOT, cwd));
  hashes.set(createHash('sha1').update(real).digest('hex').slice(0, 10), `#${cwd}#`);
};
const scrub = (s) => {
  let out = s.split(ROOT).join('$ROOT');
  for (const [h, name] of hashes) out = out.split(h).join(name);
  return out;
};
const scrubDeep = (v) => (typeof v === 'string' ? scrub(v) : Array.isArray(v) ? v.map(scrubDeep)
  : v && typeof v === 'object' ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, scrubDeep(x)])) : v);

const rawName = (buf) => {
  const s = buf.toString('utf8');
  return Buffer.from(s).equals(buf) ? { path: s } : { pathB64: buf.toString('base64') };
};

// The working folder: every entry but .git folders, by raw name; files by mode and content hash, links by target.
const dumpTree = (dir) => {
  const out = [];
  const walk = (abs, rel) => {
    const names = fs.readdirSync(abs, { encoding: 'buffer' }).sort(Buffer.compare);
    for (const name of names) {
      if (name.toString() === '.git') continue;
      const relName = rel.length ? Buffer.concat([rel, Buffer.from('/'), name]) : name;
      const full = Buffer.concat([Buffer.from(abs + '/'), name]);
      const st = fs.lstatSync(full);
      const entry = rawName(relName);
      if (st.isSymbolicLink()) out.push({ ...entry, link: fs.readlinkSync(full, { encoding: 'buffer' }).toString('base64') });
      else if (st.isDirectory()) {
        out.push({ ...entry, dir: true });
        walk(full.toString(), relName);
      } else out.push({ ...entry, mode: st.mode & 0o777, sha: createHash('sha1').update(fs.readFileSync(full)).digest('hex') });
    }
  };
  walk(dir, Buffer.alloc(0));
  return out;
};

// The shadow repository: its refs (checkpoints by tree and subject, saved edits by blob) and its own files.
const dumpRepo = (dir) => {
  if (!fs.existsSync(dir)) return null;
  const refs = run(['for-each-ref', '--format=%(refname)%00%(objecttype)%00%(tree)%00%(contents:subject)%00%(parent)%00%(objectname)'], ROOT, { GIT_DIR: dir })
    .split('\n').filter(Boolean).map((line) => {
      const [ref, type, tree, subject, parent, object] = line.split('\0');
      const name = ref.replace(`refs/${OLD_NS}/`, 'refs/NS/');
      return type === 'commit' ? { ref: name, tree, subject, parents: parent ? parent.split(' ').length : 0 } : { ref: name, type, object };
    });
  const files = {};
  for (const f of ['HEAD', 'config', 'root', 'info/exclude', 'info/attributes', 'rewinds.json', 'description']) {
    const p = path.join(dir, f);
    files[f] = fs.existsSync(p) ? scrub(fs.readFileSync(p, 'utf8')) : null;
  }
  const leftovers = fs.readdirSync(dir).filter((n) => n.endsWith('.tmp')).sort();
  return { refs, files, leftovers };
};

// --- the store cases ---

const view = (c) => ({ id: c.id, kind: c.kind, at: c.at, tree: c.tree, files: c.files });

const runStore = async (script) => {
  const scopes = {};
  const results = [];
  for (const step of script.steps) {
    if (step.now !== undefined) globalThis.__now = step.now;
    let result = null;
    const sc = step.scope ? scopes[step.scope] : null;
    switch (step.op) {
      case 'fs':
        applyActions(step.actions);
        break;
      case 'scope': {
        try {
          const s = await lib.checkpointScope(step.tab, path.join(ROOT, step.cwd), { baseDir: path.join(DATA, 'checkpoints'), limits: script.limits[step.limits ?? 'default'] });
          if (step.name) scopes[step.name] = s;
          result = { ok: true, dir: scrub(s.dir), root: scrub(s.root) };
        } catch {
          result = { ok: false };
        }
        break;
      }
      case 'take': {
        const r = await lib.takeCheckpoint(sc, { now: step.now });
        result = r.ok ? { ok: true, checkpoint: view(r.checkpoint) } : r;
        break;
      }
      case 'list':
        result = (await lib.listCheckpoints(sc)).map(view);
        break;
      case 'rewinds':
        result = await lib.listRewinds(sc);
        break;
      case 'diff':
        result = await lib.diffToCheckpoint(sc, step.id).catch(() => ({ error: true }));
        break;
      case 'rewind': {
        const done = new Set();
        const r = await lib.rewindToCheckpoint(sc, step.id, {
          isBusy: async () => Boolean(step.busy),
          beforeApply: async () => applyActions(step.beforeApply),
          beforeReplace: async (rel) => {
            if (step.beforeReplace?.[rel] && !done.has(rel)) {
              done.add(rel);
              applyActions(step.beforeReplace[rel]);
            }
          },
        }).catch(() => ({ error: true }));
        result = r.ok ? { ok: true, before: view(r.before), changed: r.changed, skipped: r.skipped } : r;
        break;
      }
      case 'seedSaved': {
        for (let i = 0; i < step.count; i++) {
          const sha = spawnSync('git', ['hash-object', '-w', '--stdin'], { input: `seed ${i}\n`, env: gitEnv({ GIT_DIR: sc.dir }) }).stdout.toString().trim();
          run(['update-ref', `refs/${OLD_NS}/saved/${step.at + i}-${sha.slice(0, 8)}`, sha], ROOT, { GIT_DIR: sc.dir });
        }
        break;
      }
      case 'seedFile':
        fs.writeFileSync(path.join(sc.dir, step.file), step.text);
        break;
      case 'dump':
        result = { repo: dumpRepo(sc.dir), tree: dumpTree(sc.root), repos: (fs.existsSync(path.join(DATA, 'checkpoints')) ? fs.readdirSync(path.join(DATA, 'checkpoints')) : []).sort().map(scrub) };
        break;
      default:
        throw new Error(`unknown op ${step.op}`);
    }
    results.push(scrubDeep(result));
  }
  return results;
};

const text = (n, seed) => Array.from({ length: n }, (_, i) => `line ${i} of ${seed} ${'x'.repeat((i * 7 + seed) % 23)}`).join('\n') + '\n';
const big = 'é€😀 the quick brown fox\n';

const storeScript = () => {
  const w = (p, s, extra = {}) => ({ path: p, write: s, ...extra });
  const steps = [];
  const fsStep = (...actions) => steps.push({ op: 'fs', actions });
  for (const cwd of ['work-a', 'work-b', 'work-c']) fsStep({ path: cwd, mkdir: true });
  fsStep(
    w('work-a/README.md', text(30, 1)),
    w('work-a/src/main.go', text(40, 2)),
    w('work-a/src/util/strings.go', text(12, 3)),
    w('work-a/bin/run.sh', '#!/bin/sh\necho hi\n', { mode: 0o755 }),
    { path: 'work-a/link-to-readme', link: 'README.md' },
    { path: 'work-a/dangling', link: 'nowhere/file' },
    w('work-a/.gitignore', '*.log\nbuild/\n'),
    w('work-a/app.log', 'log\n'),
    w('work-a/build/out.bin', 'out\n'),
    { path: 'work-a/big.dat', write: '', repeat: 'b', count: 5000 },
    w('work-a/ünïcode/файл.txt', 'привет\n'),
    w('work-a/with space/a b.txt', 'spaced\n'),
    w('work-a/weird/back\\slash.txt', 'slash\n'),
    w('work-a/weird/tab\there.txt', 'tab\n'),
    w('work-a/node_modules/pkg/index.js', 'module.exports = 1;\n'),
    w('work-a/.GIT/x', 'nope\n'),
    { pathB64: Buffer.from('work-a/bad-\xff-name.txt', 'latin1').toString('base64'), write: '', b64: Buffer.from('bad name\n').toString('base64') },
    w('work-a/binary.bin', '', { b64: Buffer.from([0, 1, 2, 3, 0, 255, 10]).toString('base64') }),
    w('work-a/latin1.txt', '', { b64: Buffer.from('caf\xe9 cr\xe8me\n', 'latin1').toString('base64') }),
    w('work-a/emoji.txt', big.repeat(10)),
    w('work-a/notes/todo.txt', 'todo\n'),
    w('work-a/gen/f1.txt', text(90, 11)),
    w('work-a/gen/f2.txt', text(90, 12)),
    w('work-a/gen/f3.txt', text(90, 13)),
    w('work-a/deep/a/b/c.txt', 'deep\n'),
    w('work-a/keep/dir/x.txt', 'x\n'),
  );
  fsStep({ path: 'work-a/nested', mkdir: true }, { path: 'work-a/nested', git: ['init', '-q'] }, w('work-a/nested/inner.txt', 'inner\n'));
  steps.push({ op: 'scope', name: 'a', tab: 'tab-a', cwd: 'work-a' });
  steps.push({ op: 'take', scope: 'a', now: T0 });
  steps.push({ op: 'take', scope: 'a', now: T0 + 1000 });
  steps.push({ op: 'list', scope: 'a' }, { op: 'rewinds', scope: 'a' }, { op: 'dump', scope: 'a' });
  fsStep(
    w('work-a/README.md', text(31, 4)),
    { path: 'work-a/src/util/strings.go', rm: true },
    { path: 'work-a/bin/run.sh', chmod: 0o644 },
    w('work-a/link-to-readme', 'now a file\n'),
    w('work-a/new/dir/file.txt', 'new\n'),
    w('work-a/app.log', 'log 2\n'),
    { path: 'work-a/big.dat', write: '', repeat: 'b', count: 100 },
    w('work-a/emoji.txt', big.repeat(12)),
    w('work-a/.gitignore', '*.log\nbuild/\nnotes/\n'),
    { path: 'work-a/gen', rm: true },
    { path: 'work-a/deep', rm: true },
    w('work-a/latin1.txt', '', { b64: Buffer.from('caf\xe9 cr\xe8me br\xfbl\xe9e\n', 'latin1').toString('base64') }),
  );
  steps.push({ op: 'take', scope: 'a', now: T0 + 5000 });
  steps.push({ op: 'list', scope: 'a' });
  steps.push({ op: 'diff', scope: 'a', id: `${T0}-turn` });
  steps.push({ op: 'diff', scope: 'a', id: `${T0 + 5000}-turn` });
  steps.push({ op: 'diff', scope: 'a', id: '1-turn' }, { op: 'diff', scope: 'a', id: 'nope' });
  steps.push({ op: 'dump', scope: 'a' });
  // A plain rewind to the first snapshot, then its undo at the same moment (the second before-rewind id gets "-2").
  steps.push({ op: 'rewind', scope: 'a', id: `${T0}-turn`, now: T0 + 60000 });
  steps.push({ op: 'dump', scope: 'a' }, { op: 'rewinds', scope: 'a' });
  steps.push({ op: 'rewind', scope: 'a', id: `${T0 + 60000}-before-rewind`, now: T0 + 60000 });
  steps.push({ op: 'dump', scope: 'a' }, { op: 'list', scope: 'a' });
  // Races: files touched between the snapshot before the rewind and the writes, and right before each write.
  steps.push({
    op: 'rewind', scope: 'a', id: `${T0}-turn`, now: T0 + 70000,
    beforeApply: [
      w('work-a/README.md', 'edited meanwhile\n'),
      { path: 'work-a/new/dir/file.txt', rm: true },
      w('work-a/gen/f1.txt', 'recreated\n'),
      { path: 'work-a/deep', rm: true },
      w('work-a/deep', 'a file where a folder was\n'),
    ],
    beforeReplace: {
      'src/main.go': [w('work-a/src/main.go', 'raced\n')],
      'emoji.txt': [{ path: 'work-a/emoji.txt', rm: true }],
      'link-to-readme': [w('work-a/link-to-readme', 'raced link\n')],
    },
  });
  steps.push({ op: 'dump', scope: 'a' }, { op: 'rewinds', scope: 'a' });
  steps.push({ op: 'rewind', scope: 'a', id: `${T0}-turn`, now: T0 + 80000, busy: true });
  steps.push({ op: 'rewind', scope: 'a', id: `${T0 + 1}-turn`, now: T0 + 80000 });
  steps.push({ op: 'rewind', scope: 'a', id: 'bad', now: T0 + 80000 });
  steps.push({ op: 'list', scope: 'a' });
  // Trimming to the newest five; the rewind log capped at 50 records, read back as it was written.
  for (let i = 1; i <= 4; i++) {
    fsStep(w('work-a/counter.txt', `${i}\n`));
    steps.push({ op: 'take', scope: 'a', now: T0 + 100000 + i * 4000 });
  }
  steps.push({ op: 'list', scope: 'a' }, { op: 'dump', scope: 'a' });
  const odd = Array.from({ length: 49 }, (_, i) => ({ at: T0 + i, targetId: `${T0}-turn`, targetAt: T0, beforeId: 'x', changed: i, skipped: 0, extra: i % 3 ? 1.5e21 : 'ü ', '': null }));
  steps.push({ op: 'seedFile', scope: 'a', file: 'rewinds.json', text: JSON.stringify(odd) });
  steps.push({ op: 'rewinds', scope: 'a' });
  steps.push({ op: 'seedSaved', scope: 'a', count: 50, at: T0 - 1000 });
  steps.push({ op: 'rewind', scope: 'a', id: `${T0 + 116000}-turn`, now: T0 + 130000, beforeApply: [w('work-a/counter.txt', 'meanwhile\n')] });
  fsStep(w('work-a/counter.txt', 'again\n'));
  steps.push({ op: 'rewind', scope: 'a', id: `${T0 + 104000}-turn`, now: T0 + 131000, beforeReplace: { 'counter.txt': [w('work-a/counter.txt', 'raced again\n')] } });
  steps.push({ op: 'dump', scope: 'a' }, { op: 'rewinds', scope: 'a' });
  steps.push({ op: 'seedFile', scope: 'a', file: 'rewinds.json', text: '{"not":"an array"}' });
  steps.push({ op: 'rewinds', scope: 'a' });
  // Too many files for scope b's limit: the snapshot is refused and the diff fails.
  fsStep(w('work-b/one.txt', '1\n'), w('work-b/two.txt', '2\n'));
  steps.push({ op: 'scope', name: 'b', tab: 'tab-b', cwd: 'work-b', limits: 'small' });
  steps.push({ op: 'take', scope: 'b', now: T0 });
  fsStep(w('work-b/three.txt', '3\n'), w('work-b/four.txt', '4\n'));
  steps.push({ op: 'take', scope: 'b', now: T0 + 10000 }, { op: 'diff', scope: 'b', id: `${T0}-turn` }, { op: 'dump', scope: 'b' });
  // A new repository prunes the old ones it finds beside it (not its siblings of the last two weeks, nor non-repos).
  fsStep(
    { path: 'data/checkpoints/old-0123456789.git', mkdir: true }, { path: 'data/checkpoints/old-0123456789.git', mtime: T0 - 15 * 86400000 },
    { path: 'data/checkpoints/young-0123456789.git', mkdir: true }, { path: 'data/checkpoints/young-0123456789.git', mtime: T0 - 86400000 },
    { path: 'data/checkpoints/notrepo', mkdir: true }, { path: 'data/checkpoints/notrepo', mtime: T0 - 30 * 86400000 },
    w('work-c/only.txt', 'c\n'),
  );
  steps.push({ op: 'scope', name: 'c', tab: 'tab-c', cwd: 'work-c' }, { op: 'take', scope: 'c', now: T0 + 200000 }, { op: 'dump', scope: 'c' });
  // Folders a scope refuses, and one reached through a link (the repository is named after the real folder).
  fsStep({ path: 'link-to-c', link: 'work-c' }, w('a-file', 'f\n'));
  steps.push(
    { op: 'scope', tab: 'bad id!', cwd: 'work-a' }, { op: 'scope', tab: 'tab-x', cwd: 'home' }, { op: 'scope', tab: 'tab-x', cwd: '../../../..' },
    { op: 'scope', tab: 'tab-x', cwd: 'missing' }, { op: 'scope', tab: 'tab-x', cwd: 'a-file' }, { op: 'scope', tab: 'tab-c', cwd: 'link-to-c' },
    { op: 'scope', tab: 'x'.repeat(128), cwd: 'work-c' }, { op: 'scope', tab: 'x'.repeat(129), cwd: 'work-c' },
  );
  return {
    roots: ['work-a', 'work-b', 'work-c'],
    limits: {
      default: { maxFileBytes: 4096, maxFiles: 200, maxCheckpoints: 5, minIntervalMs: 3000, maxDiffBytes: 6000, gitTimeoutMs: 30000 },
      small: { maxFileBytes: 4096, maxFiles: 3, maxCheckpoints: 5, minIntervalMs: 3000, maxDiffBytes: 6000, gitTimeoutMs: 30000 },
    },
    steps,
  };
};

// --- requests through the old API dispatcher ---

const TOKEN = 'a'.repeat(64);
fs.writeFileSync(path.join(DATA, 'cli-token'), TOKEN);
const routes = ['checkpoints', 'changes'].map((name) => ({ route: `${OLD_PREFIX}/${name}`, load: () => import(`@/pages${OLD_PREFIX}/${name}`) }));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const KEEP = ['content-type', 'etag', 'allow', 'cache-control'];
let lastEtag = '';
const send = async ({ method, url, headers = {}, body }) => {
  const h = {};
  for (const [k, v] of Object.entries(headers)) h[k === TOKEN_HEADER ? OLD_HEADER : k] = v === 'TOKEN' ? TOKEN : v === 'ETAG' ? lastEtag : v;
  const target = base + url.replace(PREFIX, OLD_PREFIX).split('$ROOT').join(ROOT);
  const payload = body === undefined ? undefined : body.padding ? `{"x":"${'a'.repeat(body.padding)}"}` : body.split('$ROOT').join(ROOT);
  const res = await new Promise((resolve, reject) => {
    const req = http.request(target, { method, headers: payload === undefined ? h : { ...h, 'content-length': Buffer.byteLength(payload) } }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, text: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end(payload);
  });
  if (res.headers.etag) lastEtag = res.headers.etag;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = String(res.headers[k]);
  const answer = scrub(res.text);
  // A long answer is kept as its hash and length.
  if (answer.length > 20000) return { status: res.status, answerHeaders: kept, answerSha1: createHash('sha1').update(answer).digest('hex'), answerLength: Buffer.byteLength(answer) };
  return { status: res.status, answerHeaders: kept, answer };
};

const runRequests = async (steps) => {
  const out = [];
  for (const step of steps) {
    if (step.now !== undefined) globalThis.__now = step.now;
    if (step.tabs) {
      globalThis.__golden.tabs = step.tabs.map((t) => ({ tabId: t.tabId, tmuxSession: t.session, cliState: t.state, panelType: 'claude-code' }));
      globalThis.__golden.cwds = Object.fromEntries(Object.entries(step.cwds ?? {}).map(([s, c]) => [s, path.join(ROOT, c)]));
      // A tab whose agent starts working once the handler has looked at it.
      for (const t of step.tabs) if (t.flips) {
        const entry = globalThis.__golden.tabs.find((x) => x.tabId === t.tabId);
        let seen = 0;
        Object.defineProperty(entry, 'cliState', { get: () => (seen++ ? 'busy' : t.state), enumerable: true });
      }
    }
    applyActions(step.actions);
    if (step.snapshot) await snapshotTurnEnd(step.snapshot.tab, path.join(ROOT, step.snapshot.cwd));
    out.push(step.request ? await send(step.request) : null);
  }
  return out;
};

const ses = { cookie: 'session-token=good' };
const tok = { [TOKEN_HEADER]: 'TOKEN' };
const json = { 'content-type': 'application/json' };

const checkpointsScript = () => {
  const u = (q) => `${PREFIX}/checkpoints${q}`;
  const tabs = [
    { tabId: 'tab-h', session: 'sess-h', state: 'idle' }, { tabId: 'tab-busy', session: 'sess-h', state: 'busy' },
    { tabId: 'tab-wait', session: 'sess-h', state: 'needs-input' }, { tabId: 'tab-nocwd', session: 'sess-none', state: 'idle' },
    { tabId: 'tab-home', session: 'sess-home', state: 'idle' }, { tabId: 'tab-flip', session: 'sess-h', state: 'idle', flips: true },
  ];
  const cwds = { 'sess-h': 'api-h', 'sess-home': 'home' };
  const steps = [];
  const req = (method, url, headers, body) => steps.push({ request: { method, url, headers, body } });
  steps.push({ tabs, cwds, actions: [{ path: 'api-h/a.txt', write: 'one\n' }, { path: 'api-h/sub/b.txt', write: 'two\n' }] });
  req('GET', u('?tabId=tab-h'), {});
  req('GET', u('?tabId=tab-h'), tok);
  req('GET', u(''), ses);
  req('GET', u('?tabId=bad%20id'), ses);
  req('GET', u('?tabId=tab-h&id=bad'), ses);
  req('GET', u('?tabId=tab-h&id='), ses);
  req('GET', u('?tabId=tab-h&tabId=tab-h'), ses);
  req('GET', u('?tabId=tab-h&id=1-turn&id=2-turn'), ses);
  req('GET', u('?tabId=nobody'), ses);
  req('GET', u('?tabId=tab-nocwd'), ses);
  req('GET', u('?tabId=tab-home'), ses);
  req('GET', u('?tabId=tab-h'), ses);
  steps.push({ now: T0, snapshot: { tab: 'tab-h', cwd: 'api-h' } }, { now: T0, snapshot: { tab: 'tab-flip', cwd: 'api-h' } });
  steps.push({ now: T0 + 1000, snapshot: { tab: 'tab-h', cwd: 'api-h' } });
  req('GET', u('?tabId=tab-h'), ses);
  req('GET', u('?tabId=tab-h'), { ...ses, 'if-none-match': 'ETAG' });
  req('GET', `/agents${u('?tabId=tab-busy')}`, ses);
  steps.push({ actions: [{ path: 'api-h/a.txt', write: 'one changed\n' }, { path: 'api-h/c.txt', write: 'three\n' }, { path: 'api-h/sub/b.txt', rm: true }] });
  req('GET', u(`?tabId=tab-h&id=${T0}-turn`), ses);
  req('GET', u(`?tabId=tab-h&id=${T0 + 1}-turn`), ses);
  req('GET', u(`?tabId=tab-h&id=${T0}-turn&x=1`), { ...ses, 'if-none-match': 'ETAG' });
  req('POST', u(''), ses, `{"tabId":"tab-h","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, 'content-type': 'text/plain' }, `{"tabId":"tab-h","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, 'content-type': 'APPLICATION/JSON' }, `{"tabId":"tab-h","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, ...json }, '{"tabId":');
  req('POST', u(''), { ...tok, ...json }, '{"tabId":');
  req('POST', u(''), { ...tok, ...json }, '{}');
  req('POST', u(''), { ...ses, ...json }, '');
  req('POST', u(''), { ...ses, ...json }, 'null');
  req('POST', u(''), { ...ses, ...json }, '["tab-h"]');
  req('POST', u(''), { ...ses, ...json }, '{"tabId":"tab-h"}');
  req('POST', u(''), { ...ses, ...json }, '{"tabId":"tab-h","id":null}');
  req('POST', u(''), { ...ses, ...json }, '{"tabId":"tab-h","id":"x"}');
  req('POST', u(''), { ...ses, 'content-type': 'application/jsonx' }, `{"tabId":"tab-h","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, ...json }, `{"tabId":"tab-busy","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, ...json }, `{"tabId":"tab-wait","id":"${T0}-turn"}`);
  req('POST', u(''), { ...ses, ...json }, `{"tabId":"tab-h","id":"${T0 + 7}-turn"}`);
  steps.push({ now: T0 + 50000 });
  req('POST', u('?tabId=ignored'), { ...ses, 'content-type': 'application/json; charset=utf-8' }, `{"tabId":"tab-flip","id":"${T0}-turn","tabId":"tab-flip"}`);
  req('POST', u(''), { ...ses, ...json }, `{"tabId":"tab-h","id":"${T0}-turn"}`);
  req('POST', `/agents${u('')}`, { ...ses, ...json }, `{"tabId":"tab-h","id":"${T0 + 50000}-before-rewind"}`);
  req('GET', u('?tabId=tab-h'), ses);
  req('PUT', u('?tabId=tab-h'), ses);
  req('DELETE', u('?tabId=tab-h'), tok);
  req('POST', u(''), { ...ses, ...json }, { padding: 1 << 20 });
  return { roots: ['api-h'], steps };
};

const changesScript = () => {
  const D = (s) => `2025-10-09T${s}`;
  const steps = [];
  const act = (...actions) => steps.push({ actions });
  const repo = (name, ...rest) => [{ path: `repos/${name}`, mkdir: true }, { path: `repos/${name}`, git: ['init', '-q', '-b', 'main'] }, ...rest];
  const commit = (name, msg, date, ...extra) => [{ path: `repos/${name}`, git: ['add', '-A'] }, { path: `repos/${name}`, git: ['commit', '-q', '-m', msg, ...extra], date }];
  act(
    ...repo('alpha'),
    { path: 'repos/alpha/one.txt', write: text(20, 1) }, ...commit('alpha', 'first', D('08:00:00')),
    { path: 'repos/alpha/two.txt', write: text(20, 2) }, ...commit('alpha', 'second\n\nbody line', D('10:30:00')),
    { path: 'repos/alpha/two.txt', write: text(21, 2) }, ...commit('alpha', 'second, amended', D('10:31:00'), '--amend'),
    { path: 'repos/alpha', git: ['checkout', '-q', '-b', 'side'] },
    { path: 'repos/alpha/side.txt', write: 'side\n' }, ...commit('alpha', 'on a side branch', D('10:40:00')),
    { path: 'repos/alpha', git: ['checkout', '-q', 'main'] },
    { path: 'repos/alpha', git: ['merge', '-q', '--no-ff', '-m', 'merge side', 'side'], date: D('10:45:00') },
    { path: 'repos/alpha/three.txt', write: 'é\n' }, ...commit('alpha', 'third — ünïcode', D('11:00:00')),
    { path: 'repos/alpha/one.txt', write: text(22, 3) },
    { path: 'repos/alpha/new file.txt', write: 'untracked\n' }, { path: 'repos/alpha/ünï.txt', write: 'quoted name\n' },
    { path: 'repos/alpha/dir/n.txt', write: 'n\n' },
    ...repo('Beta'),
    { path: 'repos/Beta/huge.txt', write: 'start\n' }, ...commit('Beta', 'huge', D('09:00:00')),
    { path: 'repos/Beta/huge.txt', write: '', repeat: big, count: 30000 },
    ...Array.from({ length: 45 }, (_, i) => ({ path: `repos/Beta/u/f${String(i).padStart(2, '0')}.txt`, write: `${i}\n` })),
    ...repo('gamma'), { path: 'repos/gamma/loose.txt', write: 'loose\n' },
    ...repo('delta'), { path: 'repos/delta/old.txt', write: 'old\n' }, ...commit('delta', 'old', D('01:00:00')),
    ...['r1', 'r2', 'r3', 'r4', 'r5'].flatMap((r) => [...repo(r), { path: `repos/${r}/f.txt`, write: `${r}\n` }, ...commit(r, r, D('02:00:00'))]),
    { path: 'plain/file.txt', write: 'not in a repo\n' },
    { path: 'home/.claude/projects/p/s1.jsonl', write: `not json\n{"type":"x"}\n\n{"timestamp":"${D('10:00:00.000Z')}"}\n{"timestamp":"${D('12:00:00.000Z')}"}\n` },
    { path: 'home/.claude/projects/p/s2.jsonl', write: `{"timestamp":${Date.parse(D('10:35:00Z'))}}\n` },
    { path: 'home/.claude/projects/p/none.jsonl', write: '{"timestamp":"never"}\nnull\n[1]\n{"timestamp":-5}\n' },
    { path: 'home/.claude/projects/p/long.jsonl', write: '', repeat: 'y', count: 300000 },
    { path: 'home/.claude/projects/p/dir.jsonl', mkdir: true },
    { path: 'home/.codex/sessions/2025/c.jsonl', write: `{"timestamp":"${D('10:44:00Z')}"}\n` },
    { path: 'outside.jsonl', write: `{"timestamp":"${D('10:00:00Z')}"}\n` },
  );
  const u = `${PREFIX}/changes`;
  const req = (method, url, headers, body) => steps.push({ request: { method, url, headers, body } });
  const paths = ['$ROOT/repos/alpha/one.txt', '$ROOT/repos/alpha/missing/x.txt', '$ROOT/plain/file.txt', 'relative/x',
    '$ROOT/repos/gamma/', '$ROOT/repos/delta', '$ROOT/repos/alpha/one.txt', '$ROOT/nowhere/at/all'];
  const body = (p, t) => JSON.stringify({ paths: p, jsonlPath: t });
  req('POST', u, json, body(paths, null));
  req('POST', u, { ...tok, ...json }, body(paths, null));
  req('POST', u, { ...tok, ...json }, '{"paths":');
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/s1.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/s2.jsonl'));
  req('POST', `/agents${u}`, { ...ses, ...json }, body(paths, '$ROOT/home/.codex/sessions/2025/c.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/none.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/long.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/dir.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/outside.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/../../../../outside.jsonl'));
  req('POST', u, { ...ses, ...json }, body(paths, '$ROOT/home/.claude/projects/p/missing.jsonl'));
  req('POST', u, { ...ses, ...json }, body(['$ROOT/repos/Beta/huge.txt', '$ROOT/repos/gamma'], '$ROOT/home/.claude/projects/p/s1.jsonl'));
  req('POST', u, { ...ses, ...json }, body(['$ROOT/repos/r1/f.txt', '$ROOT/repos/r2', '$ROOT/repos/r3/', '$ROOT/repos/r4/f.txt', '$ROOT/repos/r5/f.txt',
    '$ROOT/repos/delta/old.txt', '$ROOT/repos/gamma/loose.txt', '$ROOT/repos/alpha/three.txt', '$ROOT/repos/gamma/x'], '$ROOT/home/.claude/projects/p/s1.jsonl'));
  req('POST', u, { ...ses, ...json }, '{"paths":"$ROOT/repos/alpha","jsonlPath":7}');
  req('POST', u, { ...ses, ...json }, `{"paths":["$ROOT/repos/alpha",5,null,"$ROOT/repos/alpha\\u0000x"]}`);
  req('POST', u, { ...ses, ...json }, '');
  req('POST', u, { ...ses, 'content-type': 'text/plain' }, body(paths, null));
  req('POST', u, { ...ses, 'content-type': 'application/x-www-form-urlencoded' }, 'paths=$ROOT/repos/alpha&paths=$ROOT/repos/gamma&jsonlPath=$ROOT/home/.claude/projects/p/s1.jsonl');
  req('POST', u, { ...ses, 'content-type': 'application/x-www-form-urlencoded' }, 'paths=$ROOT/repos/alpha');
  req('POST', u, { ...ses, ...json, 'if-none-match': 'ETAG' }, body(paths, null));
  req('GET', `${u}?root=$ROOT/repos/alpha&sha=HEADSHA`, ses);
  return { roots: [], steps };
};

// --- run ---

const write = (rel, data) => {
  const target = path.join(path.dirname(new URL(import.meta.url).pathname), rel);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, JSON.stringify(data, null, 1) + '\n');
  console.log(`→ ${target}`);
};

if (process.env.GOLDEN_RANDOM) {
  const { randomScripts } = await import(pathToFileURL(path.resolve(need('GOLDEN_RANDOM_MODULE'))).href);
  const cases = [];
  for (const script of randomScripts(Number(process.env.GOLDEN_RANDOM), T0)) {
    fs.rmSync(path.join(DATA, 'checkpoints'), { recursive: true, force: true });
    for (const r of script.roots) { fs.rmSync(path.join(ROOT, r), { recursive: true, force: true }); fs.mkdirSync(path.join(ROOT, r), { recursive: true }); noteRoot(r); }
    cases.push({ script, results: await runStore(script) });
  }
  fs.writeFileSync(need('GOLDEN_OUT'), JSON.stringify(cases));
  console.log(`${cases.length} random cases → ${process.env.GOLDEN_OUT}`);
} else {
  const store = storeScript();
  for (const r of store.roots) { fs.mkdirSync(path.join(ROOT, r), { recursive: true }); noteRoot(r); }
  write('../../../../internal/checkpoints/testdata/golden.json', { script: store, results: await runStore(store) });

  fs.rmSync(path.join(DATA, 'checkpoints'), { recursive: true, force: true });
  const cp = checkpointsScript();
  for (const r of cp.roots) { fs.mkdirSync(path.join(ROOT, r), { recursive: true }); noteRoot(r); }
  const cpResults = await runRequests(cp.steps);
  write('../checkpoints-golden.json', { token: TOKEN, steps: cp.steps.map((s, i) => ({ ...s, answer: cpResults[i] })) });

  const ch = changesScript();
  applyActions(ch.steps[0].actions);
  const head = run(['rev-parse', 'HEAD'], path.join(ROOT, 'repos/alpha')).trim();
  for (const s of ch.steps) if (s.request) s.request.url = s.request.url.replace('HEADSHA', head);
  const chResults = await runRequests(ch.steps.slice(1));
  write('../changes-golden.json', { token: TOKEN, steps: ch.steps.map((s, i) => ({ ...s, answer: i ? chResults[i - 1] : null })) });
}
server.close();
fs.rmSync(ROOT, { recursive: true, force: true });
