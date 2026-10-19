// Runs the previous runtime's check and gallery-picture functions on fixed inputs and fixture files and writes what
// they answered to ../check-golden.json; the Go test feeds the same inputs to its port and must answer the same.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> DESIGN_LIB_MODULE=<its design library module folder> \
//   GOLDEN_STUB_DIR=$PWD node --no-warnings --experimental-transform-types \
//     --import ../../../coreapi/layoutapi/testdata/gen/register.mjs gen.mjs
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

for (const k of Object.keys(process.env)) if (/WORKSPACE|DESIGN_LIBRARY|DESIGN_HISTORY/.test(k) && k !== 'DESIGN_LIB_MODULE') delete process.env[k];
const home = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'check-golden-')));
process.env.HOME = home;
const mod = (name) => import(`@/lib/${process.env.DESIGN_LIB_MODULE}/${name}`);
const check = await mod('check');
const thumbs = await mod('thumbs');
const { libraryRoot } = await mod('paths');
const { readArtifact } = await mod('manifest');
const root = libraryRoot();
const rel = (p) => (p === null ? null : p.split(root).join('<root>'));
const api = (s) => (typeof s === 'string' ? s.replace(/^\/api\/[a-z]+\/design\//, '<api>/design/') : s);

const dom = [
  { nodeType: 9, nodeName: '#document', children: [{ nodeType: 1, nodeName: 'HTML', children: [{ nodeType: 1, nodeName: 'HEAD', children: [{ nodeType: 3, nodeName: '#text', nodeValue: 'title text' }] }, { nodeType: 1, nodeName: 'BODY', children: [] }] }] },
  { nodeType: 9, nodeName: '#document', children: [{ nodeType: 1, nodeName: 'BODY', children: [{ nodeType: 3, nodeName: '#text', nodeValue: '  Привет 👋  ' }, { nodeType: 1, nodeName: 'SCRIPT', children: [{ nodeType: 3, nodeName: '#text', nodeValue: 'var x = 1;' }] }] }] },
  { nodeType: 9, nodeName: '#document', children: [{ nodeType: 1, nodeName: 'X-DC', shadowRoots: [{ nodeType: 11, nodeName: '#document-fragment', children: [{ nodeType: 1, nodeName: 'svg' }, { nodeType: 1, nodeName: 'IMG' }] }] }] },
  { nodeType: 9, nodeName: '#document', children: [{ nodeType: 1, nodeName: 'IFRAME', contentDocument: { nodeType: 9, nodeName: '#document', children: [{ nodeType: 3, nodeName: '#text', nodeValue: 'inside frame' }, { nodeType: 1, nodeName: 'CANVAS' }] } }] },
  { nodeType: 9, nodeName: '#document', children: [{ nodeType: 1, nodeName: 'TEMPLATE', children: [{ nodeType: 1, nodeName: 'VIDEO' }] }, { nodeType: 3, nodeName: '#text' }, { nodeType: 3, nodeName: '#text', nodeValue: ' ab ' }] },
];
const statuses = [];
for (const errors of [[], ['e']]) for (const failed of [[], ['f']]) for (const blank of [false, true]) statuses.push({ errors, failed, blank, status: check.shotStatus(errors, failed, blank) });
const worst = [[], ['ok'], ['ok', 'warning'], ['warning', 'blank'], ['blank', 'error', 'ok'], ['ok', 'ok']].map((s) => ({ in: s, out: check.worstStatus(s) }));
const boards = [
  { path: 'Main.dc.html', w: null, h: null }, { path: 'lib/page.html?id=a&v=Desktop&theme=dark&m=1', w: null, h: null },
  { path: 'lib/page.html?m=10', w: null, h: null }, { path: 'B.dc.html', w: 100, h: 5000 }, { path: 'C.dc.html', w: 1920, h: null },
  { path: 'Доска 1.dc.html', w: 1440, h: 1024 },
].map((b) => ({ board: b, view: thumbs.thumbView(b), name: thumbs.thumbFileName(b.path, 'lx3k-1f') }));
const shots = [['a', '001-desktop.jpg'], ['a', '999-phone.jpg'], ['a', '01-desktop.jpg'], ['A', '001-desktop.jpg'], ['a', '001-tablet.jpg'], ['a', '../001-desktop.jpg'], ['ok-id', '123-phone.jpg']]
  .map(([ref, file]) => ({ ref, file, path: rel(check.checkShotPath(ref, file)) }));

// Fixture files: written here with fixed times, recreated by the Go test from the same list.
const T = Date.parse('2026-10-01T10:00:00.000Z') / 1000;
const report = (over) => JSON.stringify({ artifact: 'x', startedAt: 1760000000123, finishedAt: 1760000060000, done: 4, total: 4, boards: [], cover: '001-desktop.jpg', failure: null, ...over }, null, 2);
const boardOf = (title, status) => ({ title, path: `${title}.dc.html`, status, shots: [{ view: 'desktop', status, screenshot: '001-desktop.jpg', errors: status === 'error' ? ['TypeError: x is <undefined> & more'] : [], failedRequests: [], ms: 1234 }, { view: 'phone', status: 'ok', screenshot: null, errors: [], failedRequests: ['404 http://127.0.0.1:1/a/b.png'], ms: 99 }] });
const files = [
  ['alpha/project/Main.dc.html', '<html>main</html>', 0],
  ['alpha/project/Second.dc.html', '<html>second</html>', 60],
  ['alpha/.check/report.json', report({ artifact: 'alpha', boards: [boardOf('Main', 'error'), boardOf('Second', 'warning'), boardOf('Third', 'blank'), boardOf('Fourth', 'ok')] }), 0],
  ['beta/project/Only.dc.html', '<b>x</b>', 0],
  ['beta/.check/report.json', report({ artifact: 'beta', cover: null, finishedAt: null, failure: 'На машине нет Chrome', done: 0, total: 2, extra: { kept: true } }), 0],
  ['gamma/project/One.dc.html', 'one', 0],
  ['gamma/.check/report.json', report({ artifact: 'gamma', cover: '', startedAt: 7 }), 0],
  ['delta/project/A.dc.html', 'a', 0],
  ['delta/.check/report.json', '{broken', 0],
  ['eps/project/lib/page.html', '<page>', 10],
  ['eps/project/lib/pages.js', 'js', 500],
  ['eps/project/lib/nested/deep.js', 'deep', 900],
  ['eps/project/Wrap.dc.html', 'wrapper', 20],
  ['eps/artifact.json', JSON.stringify({ title: 'Eps', boards: [{ title: 'Page', path: 'lib/page.html?id=p&v=Desktop' }, { title: 'Wrap', path: 'Wrap.dc.html' }] }), 0],
];
for (const [name, content, offset] of files) {
  const file = path.join(root, name);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content);
  fs.utimesSync(file, T + offset, T + offset + 0.123); // atime, mtime
}
const attached = [];
for (const id of ['alpha', 'beta', 'gamma', 'delta', 'eps']) {
  const [a] = await check.attachChecks([await readArtifact(id)]);
  attached.push({ id, cover: api(a.cover), check: a.check });
}
const reports = [];
for (const id of ['alpha', 'beta', 'gamma', 'delta', 'nope']) reports.push({ id, json: JSON.stringify({ report: await check.readCheckReport(id) }) });
const versions = [];
for (const [id, board] of [['alpha', 'Main.dc.html'], ['alpha', 'Second.dc.html'], ['eps', 'lib/page.html?id=p&v=Desktop'], ['eps', 'Wrap.dc.html'], ['eps', 'Missing.dc.html'], ['Bad', 'x.html']]) {
  versions.push({ id, board, version: await thumbs.thumbVersion(id, board) });
}
const sample = { artifact: 'alpha', startedAt: 1, finishedAt: null, done: 1, total: 2, boards: [boardOf('Main', 'error'), { title: 'Line sep', path: 'L.dc.html', status: 'ok', shots: [] }], cover: null, failure: null };
await check.saveCheckReport(sample);
const saved = fs.readFileSync(path.join(root, 'alpha', '.check', 'report.json'), 'utf8');

fs.writeFileSync(new URL('../check-golden.json', import.meta.url), `${JSON.stringify({ dom: dom.map((d) => ({ dom: d, content: check.pageContent(d) })), statuses, worst, boards, shots, files: files.map(([name, content, offset]) => ({ name, content, mtime: T + offset + 0.123 })), attached, reports, versions, sample, saved }, null, 2)}\n`);
fs.rmSync(home, { recursive: true, force: true });
