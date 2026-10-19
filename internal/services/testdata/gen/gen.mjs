// Sends a fixed list of requests to the previous runtime's own API dispatcher and handlers, with every external service
// (GitLab, the tracker, published pages, sites and their icons) played by a local fake from fixtures, and writes what
// each answered to ../golden.json and the fixtures to ../fixtures.json. The Go test serves the same fixtures, sends the
// same requests to its handlers and must answer byte for byte.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_RENAMES=<its names=neutral names,...> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { cases, fixtureSet } from './cases.mjs';

const FIXED = Date.parse('2026-10-18T15:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const here = path.dirname(new URL(import.meta.url).pathname);
const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'services-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
for (const k of Object.keys(process.env)) if (/TOKEN|^GIT_/.test(k)) delete process.env[k];
process.env.HOME = home;
delete process.env.HOST;
process.env.ACME_BOUND_HOST = '127.0.0.1';
process.env.GIT_CONFIG_NOSYSTEM = '1';
const built = spawnSync('sh', [path.join(here, 'repos.sh'), root], { stdio: 'inherit' });
if (built.status !== 0) throw new Error('repos.sh failed');
const sha = (repo, ref) => spawnSync('git', ['-C', path.join(root, 'work', repo), 'rev-parse', ref], { encoding: 'utf8' }).stdout.trim();
const fixtures = fixtureSet(sha);

// The fake: a fixture per "host target"; a missing one is a 404 and is reported.
const missing = new Set();
const generated = (g) => {
  if (g.kind === 'bytes') return Buffer.from(Array.from({ length: g.count }, (_, i) => i % 251));
  if (g.kind === 'files') {
    return Buffer.from(JSON.stringify(Array.from({ length: g.count }, (_, i) => ({
      old_path: `gen/f${i}.txt`, new_path: `gen/f${i}.txt`, new_file: false, deleted_file: false, renamed_file: false,
      diff: `@@ -1 +1,${g.lines} @@\n${Array.from({ length: g.lines }, (_, j) => `+line ${j} of file ${i}`).join('\n')}\n`,
    }))));
  }
  throw new Error(`unknown generator ${g.kind}`);
};
const fake = http.createServer((req, res) => {
  const key = `${req.headers['x-fake-host']} ${req.url}`;
  const f = fixtures[key];
  if (!f) {
    missing.add(key);
    res.writeHead(404, { 'content-type': 'text/plain' });
    res.end('no fixture');
    return;
  }
  if (f.drop) {
    req.socket.destroy();
    return;
  }
  if (f.auth && req.headers[f.auth[0]] !== f.auth[1]) {
    res.writeHead(401, { 'content-type': 'application/json' });
    res.end('{"message":"401 Unauthorized"}');
    return;
  }
  res.writeHead(f.status ?? 200, f.headers ?? {});
  res.end(f.gen ? generated(f.gen) : f.bodyB64 ? Buffer.from(f.bodyB64, 'base64') : (f.body ?? ''));
});
await new Promise((r) => fake.listen(0, '127.0.0.1', r));
const fakeBase = `http://127.0.0.1:${fake.address().port}`;

// fetch reaches every host through the fake, following or refusing redirects itself, as fetch does.
const realFetch = globalThis.fetch;
const REDIRECTS = new Set([301, 302, 303, 307, 308]);
globalThis.fetch = async (input, init = {}) => {
  let url = new URL(input instanceof URL ? input.href : String(input));
  const mode = init.redirect ?? 'follow';
  const headers = Object.fromEntries(new Headers(init.headers ?? {}));
  for (let hop = 0; ; hop++) {
    if (!/^https?:$/.test(url.protocol)) throw new TypeError('fetch failed');
    const res = await realFetch(`${fakeBase}${url.pathname}${url.search}`, {
      headers: { ...headers, 'x-fake-host': url.host }, redirect: 'manual', signal: init.signal,
    });
    if (REDIRECTS.has(res.status)) {
      if (mode === 'error') throw new TypeError('fetch failed');
      const location = res.headers.get('location');
      if (location !== null) {
        if (hop >= 20) throw new TypeError('fetch failed');
        await res.arrayBuffer();
        url = new URL(location, url);
        continue;
      }
    }
    const body = [204, 205, 304].includes(res.status) ? null : await res.arrayBuffer();
    const out = new Response(body, { status: res.status, statusText: res.statusText, headers: res.headers });
    Object.defineProperty(out, 'url', { value: url.href.replace(/#.*$/, '') });
    return out;
  }
};

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const { API_ROUTES } = await import('@/lib/runtime-api/routes');
const dispatch = createApiDispatcher(API_ROUTES);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const fill = (s) => s.split('$ROOT').join(root);
const scrub = (s) => s.split(root).join('$ROOT');
const apply = (setup = {}) => {
  for (const [k, v] of Object.entries(setup.env ?? {})) {
    if (v === null) delete process.env[k];
    else process.env[k] = v;
  }
  for (const [name, content] of Object.entries(setup.files ?? {})) {
    const file = path.join(root, name);
    if (content === null) fs.rmSync(file, { force: true });
    else {
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, fill(content));
    }
  }
};

const KEEP = ['content-type', 'etag', 'allow', 'cache-control', 'content-length', 'x-content-type-options', 'content-security-policy'];
const out = [];
let lastEtag = '';
for (const c of cases) {
  if (process.env.GOLDEN_DEBUG) console.error(c.method, c.url);
  apply(c.setup);
  const h = { ...(c.headers ?? {}) };
  if (h['if-none-match'] === 'ETAG') h['if-none-match'] = lastEtag;
  const body = c.body === undefined ? undefined : fill(c.body);
  const res = await new Promise((resolve, reject) => {
    const req = http.request(base + fill(c.url), { method: c.method, headers: body === undefined ? h : { ...h, 'content-length': Buffer.byteLength(body) } }, (r) => {
      const chunks = [];
      r.on('data', (d) => chunks.push(d));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, body: Buffer.concat(chunks) }));
    });
    req.on('error', reject);
    req.end(body);
  });
  if (res.headers.etag) lastEtag = res.headers.etag;
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = scrub(String(res.headers[k]));
  const text = res.body.toString('utf8');
  const binary = !Buffer.from(text, 'utf8').equals(res.body);
  const answer = res.body.length > 65536 ? { answerSha256: createHash('sha256').update(res.body).digest('hex'), answerLength: res.body.length }
    : binary ? { answerB64: res.body.toString('base64') } : { answer: scrub(text) };
  out.push({ ...c, status: res.status, answerHeaders: kept, ...answer });
}
server.close();
fake.close();
const organizer = path.join(home, '.acme-data', 'organizer.json');
const files = { organizer: fs.existsSync(organizer) ? fs.readFileSync(organizer, 'utf8') : null };
fs.writeFileSync(path.join(here, '..', 'fixtures.json'), JSON.stringify(fixtures, null, 1) + '\n');
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify({ cases: out, files }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
if (missing.size) console.error(`requests without a fixture:\n  ${[...missing].join('\n  ')}`);
console.log(`${out.length} cases`);
