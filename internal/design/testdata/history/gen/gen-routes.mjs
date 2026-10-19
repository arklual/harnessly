// Sends a fixed list of requests to the previous runtime's dispatcher and its design history and import handlers, on
// a fixture library, and writes the answers to ../routes-golden.json; the Go test sends the same to its routes.
//   OLD_RUNTIME_SRC=<checkout> OLD_ENV_PREFIX=<its env prefix> OLD_RUNTIME_TOKEN_HEADER=<its cli-token header> \
//   OLD_CLI=<its CLI name> OLD_API=<its platform API prefix> GOLDEN_STUB_DIR=$PWD \
//     node --no-warnings --experimental-transform-types --import ../../../../coreapi/layoutapi/testdata/gen/register.mjs gen-routes.mjs
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

process.umask(0o022);
const fixtures = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const P = process.env.OLD_ENV_PREFIX;
const API = process.env.OLD_API;
const tmp = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'design-routes-golden-')));
process.env.HOME = path.join(tmp, 'home');
const library = path.join(tmp, 'library');
fs.mkdirSync(path.join(library, 'p1', 'project'), { recursive: true });
fs.writeFileSync(path.join(library, 'p1', 'project', 'A.dc.html'), '<main class="board">a</main>\n');
process.env[`${P}_DESIGN_LIBRARY_DIR`] = library;
process.env[`${P}_DESIGN_HISTORY_DIR`] = path.join(tmp, 'history');
const TOKEN = 'b'.repeat(64);

const { dataDir } = await import('@/lib/data-dir');
fs.mkdirSync(dataDir(), { recursive: true });
fs.writeFileSync(path.join(dataDir(), 'cli-token'), TOKEN, { mode: 0o600 });
const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const routes = [`${API}/design/library/history`, `${API}/design/library/import`, '/api/cli/design/history']
  .map((route) => ({ route, load: () => import(`@/pages${route}`) }));
const dispatch = createApiDispatcher(routes);
const server = http.createServer((req, res) => {
  dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end(); } });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const OLD = process.env.OLD_RUNTIME_TOKEN_HEADER;
const tok = { [OLD]: TOKEN };
const ses = { cookie: 'session-token=good' };
const json = { 'content-type': 'application/json' };
const zipType = { 'content-type': 'application/zip' };
const zip = fs.readFileSync(path.join(fixtures, 'project.zip'));
const H = `${API}/design/library/history`;
const I = `${API}/design/library/import`;
const C = '/api/cli/design/history';
const requests = [
  ['GET', `${H}?artifact=p1&board=A.dc.html`, {}],
  ['GET', `${H}?artifact=p1&board=A.dc.html`, tok],
  ['PUT', H, { ...ses, ...json }, '{}'],
  ['POST', H, { ...ses, 'content-type': 'text/plain' }, 'x'],
  ['POST', H, { ...ses, ...json }, '{'],
  ['GET', `${H}?artifact=nope&board=A.dc.html`, ses],
  ['GET', `${H}?artifact=p1&board=lib/page.html`, ses],
  ['GET', `${H}?artifact=p1&artifact=p1&board=A.dc.html`, ses],
  ['POST', H, { ...ses, ...json }, '{"artifact":"p1","board":"A.dc.html","revision":"zz"}'],
  ['POST', H, { ...ses, ...json }, JSON.stringify({ artifact: 'p1', board: 'A.dc.html', revision: 'a'.repeat(40) })],
  ['POST', H, { ...ses, ...json }, JSON.stringify({ artifact: 'p1', board: 'Main', revision: 'a'.repeat(40) })],
  ['GET', C, {}],
  ['DELETE', C, tok],
  ['GET', `${C}?project=nope`, tok],
  ['GET', `${C}?project=p1&board=Nope&n=abc`, tok],
  ['GET', `${C}?project=p1&project=p2`, tok],
  ['POST', C, { ...tok, 'content-type': 'text/plain' }, 'x'],
  ['POST', C, { ...tok, ...json }, '{"action":"x"}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","session":"bad session!"}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","session":null}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","project":"p1","board":"A","at":"zz"}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","project":"p1","board":"A","at":5}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","project":"p1","board":"A/b","at":"1d"}'],
  ['POST', C, { ...tok, ...json }, '{"action":"restore","project":"p1","board":"A","at":"1d"}'],
  ['GET', I, ses],
  ['POST', I, zipType, zip],
  ['POST', I, { ...ses, 'content-type': 'text/plain' }, 'x'],
  ['POST', I, { ...ses, ...zipType }, ''],
  ['POST', I, { ...ses, ...zipType, 'x-design-filename': encodeURIComponent('Мой Сайт.zip') }, zip],
  ['POST', I, { ...ses, ...zipType, 'x-design-filename': encodeURIComponent('dir/Мой Сайт.zip') }, zip],
  ['POST', I, { ...ses, ...zipType, 'x-design-filename': '%E0%A4%A' }, zip],
  ['POST', I, { ...ses, ...zipType }, 'notzip'],
];
const kept = ['content-type', 'cache-control', 'allow', 'etag'];
const out = [];
for (const [method, url, headers, body] of requests) {
  const answer = await new Promise((resolve, reject) => {
    const h = body === undefined ? headers : { ...headers, 'content-length': Buffer.byteLength(body) };
    const req = http.request(base + url, { method, headers: h }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: Object.fromEntries(kept.filter((k) => r.headers[k] !== undefined).map((k) => [k, r.headers[k]])), body: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    if (body !== undefined) req.write(body);
    req.end();
  });
  const neutralBody = answer.body.split(process.env.OLD_CLI).join('acme-cli');
  const neutralHeaders = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k === OLD ? 'x-cli-token' : k, Buffer.isBuffer(v) ? null : v]));
  // The answer's tag is of the bytes it carried: a body with the product's name gets the tag of its neutral bytes.
  if (neutralBody !== answer.body && answer.headers.etag) answer.headers.etag = `"${crypto.createHash('sha1').update(neutralBody).digest('base64url').slice(0, 27)}"`;
  out.push({ method, url: url.split(API).join('/api/platform'), headers: neutralHeaders, body: Buffer.isBuffer(body) ? '@project.zip' : body ?? null, answer: { ...answer, body: neutralBody } });
}
const library2 = fs.readdirSync(library).sort();
fs.writeFileSync(path.join(fixtures, 'routes-golden.json'), `${JSON.stringify({ token: TOKEN, requests: out, library: library2 }, null, 1)}\n`);
server.close();
fs.rmSync(tmp, { recursive: true, force: true });
process.exit(0);
