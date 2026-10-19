// Runs the previous runtime's design history, import and new-project functions on fixtures and writes what they
// gave to ../golden.json; the Go tests run the same cases and must give the same.
//
// The previous runtime spells its environment and its words with its product's name; they come from the caller:
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_MODULE_PREFIX=<its module word> OLD_ENV_PREFIX=<its env prefix> OLD_CLI=<its CLI name> OLD_KIT=<its UI kit global> \
//   OLD_TAG=<its history tag> OLD_EMAIL=<its history address> GOLDEN_STUB_DIR=$PWD \
//     node --no-warnings --experimental-transform-types --import ../../../../coreapi/layoutapi/testdata/gen/register.mjs gen.mjs
import { execFileSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

process.umask(0o022);
const here = path.dirname(fileURLToPath(import.meta.url));
const fixtures = path.dirname(here);
const P = process.env.OLD_ENV_PREFIX;
const neutral = (s) => s.split(process.env.OLD_CLI).join('acme-cli').split(process.env.OLD_KIT).join('AcmeKit').split(process.env.OLD_TAG).join('acme-design').split(process.env.OLD_EMAIL).join('design@example.local');

const tmp = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'design-history-golden-')));
process.env.HOME = path.join(tmp, 'home');
fs.mkdirSync(process.env.HOME);
const library = path.join(tmp, 'library');
fs.mkdirSync(library);
process.env[`${P}_DESIGN_LIBRARY_DIR`] = library;
process.env[`${P}_DESIGN_HISTORY_DIR`] = path.join(tmp, 'history');

const history = await import(`@/lib/${process.env.OLD_RUNTIME_MODULE_PREFIX}-design-library/history`);
const importer = await import(`@/lib/${process.env.OLD_RUNTIME_MODULE_PREFIX}-design-library/import`);
const create = await import(`@/lib/${process.env.OLD_RUNTIME_MODULE_PREFIX}-design-library/create`);
const claims = await import(`@/lib/${process.env.OLD_RUNTIME_MODULE_PREFIX}-design-library/claims`);

const out = {};
out.snapshotMessage = [
  [['p1/project/A.dc.html', 'p1/project/B.dc.html'], [], 'rt', undefined],
  [['p1/project/A.dc.html', 'p1/project/B.dc.html', 'p1/project/C.dc.html', 'p2/project/D.dc.html', 'p1/project/A.dc.html'], ['Ann · чат «x» (rt)'], 'rt', undefined],
  [['p1/project/lib/x.js'], [], 'rt', undefined],
  [['p1/artifact.json', 'p2/project/x.css'], [], 'rt', undefined],
  [['CLAUDE.md'], ['a', 'b', 'c', 'd', 'e', 'f'], 'rt', undefined],
  [['p1/project/A.dc.html'], [], 'rt', 'Вернул «A»\u0007 к версии abc'],
  [['p1/project/sub/A.dc.html', 'P1/project/A.dc.html'], [], 'rt', ''],
].map(([paths, editors, runtime, subject]) => ({ paths, editors, runtime, subject: subject ?? null, out: history.snapshotMessage(paths, editors, runtime, subject) }));

const U = '\u001f', R = '\u001e', G = '\u001d';
const sha = 'a'.repeat(40);
out.parseLog = [
  '',
  `${R}${sha}${U}1700000000${U}Правки: «A»${U}Ann · чат «x» (rt)${G}Bob${U}rt\n`,
  `${R}${sha}${U}1700000000${U}subj${U}${U}\n\n${R}${'b'.repeat(40)}${U} 17 ${U}‮subj2${U}${G}${U}${G}rt2\n`,
  `${R}${sha}${U}x${U}s${U}${U}rt\n${R}short${U}1${U}s${U}${U}rt\n${R}${sha}\n`,
  `${R}${sha}${U}${U}empty at${U}${U}\n`,
].map((input) => ({ input, out: history.parseLog(input) }));

const tryIt = (fn) => { try { return { ok: fn() }; } catch (e) { return { error: e.message }; } };
out.scopePath = [
  ['p1', undefined], ['p1', 'A.dc.html'], ['P1', undefined], ['p1', 'a/b.dc.html'], ['p1', '.x.dc.html'], ['p1', 'x.html'], ['p1', 'a\nb.dc.html'],
].map(([project, file]) => ({ project, file: file ?? null, out: tryIt(() => history.scopePath({ project, file })) }));
out.restoreTarget = ['30m', ' 2H ', '1d', '1000m', 'abc1234', 'ABCDEF0', 'abc12', 'a'.repeat(40), 'a'.repeat(41), '', '0m']
  .map((input) => ({ input, out: history.parseRestoreTarget(input) }));
out.isHistoryPath = ['p1/project/A.dc.html', '.claims/p1/x.json', 'p1/node_modules/a', 'a.tmp', '.importing-x/a', 'x/.git/y', '', 'p1\\.check\\a']
  .map((input) => ({ input, out: history.isHistoryPath(input) }));
out.slugify = [
  ['Мой Сайт.zip', []], ['Мой Сайт.zip', ['moy-sayt', 'moy-sayt-2']], ['  ---  ', []], ['ЙОЖ ёлка.ZIP', []], ['Йx', []],
  ['a'.repeat(47) + '-bcd', []], ['Ünïcödé — test', []], ['Щука ъ ь', []], ['x.zip.zip', []],
].map(([name, taken]) => ({ name, taken, out: importer.slugifyArtifactId(name, new Set(taken)) }));
out.unzipListing = ['', 'Archive: x\n  Length Date\n---------  -------\n      388  14 files\n', '   5 1 file', 'garbage']
  .map((input) => ({ input, out: importer.parseUnzipListing(input) }));
out.cleanBrief = ['', '  \u0001 \t', 'нарисуй\u0007 экран\nвторой', 'x'.repeat(4001)].map((input) => ({ input, out: tryIt(() => create.cleanBrief(input)) }));
out.newProjectPrompt = neutral(create.buildNewProjectPrompt('  Мой \n проект ', 'бриф'));

const tree = (dir) => {
  const lines = [];
  const walk = (rel) => {
    for (const name of fs.readdirSync(path.join(dir, rel)).sort()) {
      const r = rel ? `${rel}/${name}` : name;
      const st = fs.lstatSync(path.join(dir, r));
      if (st.isDirectory()) {
        lines.push(`d ${(st.mode & 0o7777).toString(8)} ${r}`);
        walk(r);
      } else {
        let data = fs.readFileSync(path.join(dir, r), 'utf8');
        if (name === 'artifact.json') data = data.replace(/"importedAt": "[^"]*"/, '"importedAt": "<now>"');
        lines.push(`f ${(st.mode & 0o7777).toString(8)} ${r} ${name === 'artifact.json' ? JSON.stringify(data) : crypto.createHash('sha1').update(data).digest('hex')}`);
      }
    }
  };
  walk('');
  return lines;
};

const copyFixture = (from, to) => fs.cpSync(from, to, { recursive: true });
const zipCopy = path.join(tmp, 'upload.zip');
fs.copyFileSync(path.join(fixtures, 'project.zip'), zipCopy);
const imported = await importer.importDesignProject({ source: zipCopy, id: importer.slugifyArtifactId('Мой Сайт.zip'), title: '  Мой Сайт  ', sourceName: 'Мой Сайт.zip' });
out.import = { result: { id: imported.id, title: imported.title, boards: imported.boards, dropped: imported.dropped }, tree: tree(path.join(library, imported.id)) };
out.importAgain = await importer.importDesignProject({ source: zipCopy, id: imported.id, title: null }).then(() => 'ok', (e) => e.message);
out.importBroken = await importer.importDesignProject({ source: path.join(fixtures, 'template', 'artifact.json'), id: 'broken', title: null }).then(() => 'ok', (e) => e.message);

out.createMissing = await create.createDesignProject({ title: 'Новый' }).then(() => 'ok', (e) => neutral(e.message.split(library).join('<library>')));
copyFixture(path.join(fixtures, 'template'), path.join(library, '.template'));
fs.chmodSync(path.join(library, '.template', 'project', 'sub', 'x.css'), 0o640);
fs.chmodSync(path.join(library, '.template', 'project', 'sub'), 0o750);
fs.symlinkSync('/etc/hostname', path.join(library, '.template', 'project', 'link.html'));
const created = await create.createDesignProject({ title: '  Новый\u0007 проект ' });
out.create = { id: created.id, title: created.title, tree: tree(path.join(library, created.id)) };
const created2 = await create.createDesignProject({ title: 'Новый проект' });
out.create2 = created2.id;
out.createBad = await create.createDesignProject({ title: ' \u0001 ' }).then(() => 'ok', (e) => e.message);

// A history sequence on the library as it is now: first snapshot, a claimed edit, a restore by revision.
const git = (...args) => execFileSync('git', [`--git-dir=${history.historyGitDir(fs.realpathSync(library))}`, ...args], { encoding: 'utf8' });
const first = await history.snapshotNow({ runtime: 'rt' });
const board = path.join(library, imported.id, 'project', 'Board 2.dc.html');
const original = fs.readFileSync(board, 'utf8');
await claims.claimBoard({ project: imported.id, board: 'Board 2', owner: { user: 'Ann', runtime: 'rt', tabId: 'tab-1', chatTitle: 'Чат' } });
fs.writeFileSync(board, '<main class="board">changed</main>\n');
const editors = (await claims.listLiveClaims(imported.id)).map((c) => history.ownerLine(c.owner));
const second = await history.snapshotNow({ runtime: 'rt', editors });
const clean = await history.snapshotNow({ runtime: 'rt' });
const listed = await history.readHistory({ project: imported.id, file: 'Board 2.dc.html' }, 12);
const refused = await history.restoreBoard({ project: imported.id, file: 'Board 2.dc.html', at: first.sha, actor: { runtime: 'rt', line: 'Bob', owner: null } }).then(() => 'ok', (e) => e.message);
const restored = await history.restoreBoard({ project: imported.id, file: 'Board 2.dc.html', at: first.sha.slice(0, 9), actor: { runtime: 'rt', line: 'Ann · чат «Чат» (rt)', owner: { user: 'Ann', runtime: 'rt', tabId: 'tab-1', chatTitle: 'Чат' } } });
const missing = await history.restoreBoard({ project: imported.id, file: 'Nope.dc.html', at: first.sha, actor: { runtime: 'rt', line: 'x', owner: { user: 'Ann', runtime: 'rt', tabId: 'tab-1', chatTitle: 'Чат' } } }).then(() => 'ok', (e) => e.message);
const tooEarly = await history.restoreBoard({ project: imported.id, file: 'Board 2.dc.html', at: '1d', actor: { runtime: 'rt', line: 'x', owner: { user: 'Ann', runtime: 'rt', tabId: 'tab-1', chatTitle: 'Чат' } } }).then(() => 'ok', (e) => e.message);
out.history = {
  kinds: [first.kind, second.kind, clean.kind, restored.snapshot.kind],
  listed: { unsaved: listed.unsaved, entries: listed.entries.map(({ subject, editors, runtime }) => ({ subject, editors, runtime })) },
  refused, missing, tooEarly,
  restoredSame: restored.revision === first.sha,
  boardAfter: fs.readFileSync(board, 'utf8') === original,
  log: neutral(git('log', '--format=%s%n%b%n%an <%ae>%n--')),
  exclude: neutral(fs.readFileSync(path.join(history.historyGitDir(fs.realpathSync(library)), 'info', 'exclude'), 'utf8')),
  config: fs.readFileSync(path.join(history.historyGitDir(fs.realpathSync(library)), 'config'), 'utf8'),
};

fs.writeFileSync(path.join(fixtures, 'golden.json'), `${JSON.stringify(out, null, 2)}\n`);
fs.rmSync(tmp, { recursive: true, force: true });
