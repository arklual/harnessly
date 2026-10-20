// Runs the previous runtime's pure functions behind the routes on generated inputs (a seeded generator, so a run is
// repeatable) and writes inputs and outputs for the Go test: ../units.json with a few hundred cases per function, or
// UNITS_OUT=<file> UNITS_N=<count> for a large run that is not committed.
//
//   OLD_RUNTIME_SRC=... OLD_RUNTIME_RENAMES=... OLD_RUNTIME_MODULE_PREFIX=<prefix of its own module names> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs units.mjs
import fs from 'node:fs';
import path from 'node:path';

const N = Number(process.env.UNITS_N ?? 200);
let seed = Number(process.env.UNITS_SEED ?? 7);
const rnd = () => {
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const pick = (list) => list[Math.floor(rnd() * list.length)];
const some = (list, max) => Array.from({ length: Math.floor(rnd() * (max + 1)) }, () => pick(list));

const P = process.env.OLD_RUNTIME_MODULE_PREFIX ?? '';
const { parseLinkTarget } = await import('@/lib/link-meta');
const { mrRefOf, gitlabRefOf } = await import(`@/lib/${P}gitlab-ref`);
const { gitlabProjectOf, unifiedDiffOf } = await import(`@/lib/${P}gitlab`);
const { parseArtifactHtml } = await import('@/lib/link-meta-server');
const { iconCandidates, parentHost } = await import(`@/lib/${P}favicon`);
const { parseTaskIssue, cleanTaskText } = await import(`@/lib/${P}task`);
const { parseSessionStart, parseReflogCommits } = await import(`@/lib/${P}changes`);
const { parseOrganizerOps, applyOrganizerOps, emptyOrganizer, isOrganizer } = await import(`@/lib/${P}organizer`);

const HOSTS = ['gitlab.example.com', 'GitLab.Example.com', 'gitlab.com', 'gitlab.com.', 'tracker.example.com', 'youtrack.example.com',
  'docs.example.com', 'platform.example.com', 'gitlab%2Eexample.com', 'example.com', '127.0.0.1', '0x7f.1', '[::1]', '[1:0:0:0:0:0:0:1]',
  'a b.com', 'xn--a.com', '', 'gitlab.example.com:443', 'gitlab.example.com:0443', 'gitlab.example.com:8443', 'gitlab.example.com:',
  'gitlab.example.com:x', 'user@gitlab.example.com', 'u:p@docs.example.com', ':@gitlab.com', '@gitlab.com', 'a@b@tracker.example.com',
  '999.1.1.1', '1.2.3', '09.1.1.1', 'docs.example.com.', 'host_name.example.com'];
const SEGMENTS = ['group', 'sub', 'alpha', 'a', 'b', '..', '.', '%2e', '%2E%2e', '-', 'merge_requests', '7', '0012', '1234567890',
  'issue', 'ACME-12', 'acme-1', 'DEMO-1234567', 'youtrack', 'agents', 'docs', 'report-1', 'api', 'api/', 'commit', 'abcdef0',
  '1cb8a0a44c2e959bfd6168e3ffb5ddd01142385c', 'ABCDEF0', '%2F', 'ä', 'x y', '"q"', '{c}', 'a|b', 'é%zz', '', 'slug_1', 'x'.repeat(161), 'diffs'];
const QUERIES = ['', '?', '?k=key_1', '?k=bad key', '?k=', '?x=1&k=a', '?k=a+b', '?k=%41', "?k='", '?k=1&k=2', '?a=<b>', '?ä=1'];
const FRAGS = ['', '#', '#f', '#a b', '#"x"'];
const SCHEMES = ['https://', 'https:', 'HTTPS://', 'https:\\\\', 'https:///', 'http://', 'ftp://', 'javascript:', 'mailto:', '//', '', ' https://', 'h\ttps://'];

const genURL = () => `${pick(SCHEMES)}${pick(HOSTS)}${rnd() < 0.15 ? '\\' : '/'}${some(SEGMENTS, 6).join(rnd() < 0.1 ? '\\' : '/')}${pick(QUERIES)}${pick(FRAGS)}${rnd() < 0.05 ? ' \n' : ''}`;
const genRef = () => {
  const host = pick(['gitlab.example.com', 'gitlab.com', 'GITLAB.COM', 'other.example.com', 'gitlab.example.com:8443']);
  const project = some(['group', 'sub', 'x.y', 'a-b', '..', 'ä', '%2F'], 4).join('/') || 'p';
  const tail = pick(['-/merge_requests/7', '-/merge_requests/007/diffs', '-/merge_requests/', '-/commit/abcdef0', '-/commit/ABCDEF0',
    '-/commit/1cb8a0a44c2e959bfd6168e3ffb5ddd01142385c/', '-/commit/abc', 'merge_requests/1', '-/merge_requests/12#note', '-/-/merge_requests/3']);
  return `${pick(['https://', 'http://', 'https://u@'])}${host}/${project}/${tail}`;
};
const genRemote = () => pick(['', 'https://', 'ssh://', 'git+ssh://', 'HTTPS://']) + pick(['', 'git@', 'user:pw@', 'a@b@']) +
  pick(['gitlab.example.com', 'gitlab.com', 'github.com', 'gitlab.example.com:2222']) + pick([':', '/', '//']) +
  some(['group', 'sub', 'proj', 'x.git', '.git'], 3).join('/') + pick(['', '.git', '/', '.git/', ' ', '\n']);

const WORDS = ['<title>', '</title>', '<TITLE x="1">', '<meta name="description" content="d1">', '<meta property="og:description" content=\'d2\'>',
  '<meta content="x" name="description">', '<META NAME="DESCRIPTION" CONTENT="Big">', '&amp;', '&lt;', '&#x41;', '&#66;', '&#0;', '&#x110000;',
  '&#xD83D;', '&nbsp;', '&laquo;', '&Mdash;', '&unknown;', '&#xzz;', 'text', ' ', '\n', '\t', ' ', ' ', '😀', 'ſ', 'K', 'é', '<', '>', '"', "'"];
const genHTML = () => some(WORDS, 25).join('') + (rnd() < 0.05 ? 'x'.repeat(299) + '😀' + '</title>' : '');

const ICON_ATTRS = ['rel="icon"', "rel='shortcut icon'", 'REL="apple-touch-icon"', 'rel="apple-touch-icon-precomposed"', 'rel="stylesheet"',
  'rel="Icon"', 'rel="İcon"', 'rel="iconic"', 'data-rel="x"', 'href="/a.png"', 'href="b.svg"', "href='//cdn.example.org/c.png'",
  'href="https://x.example.org/d.ico?v=1#f"', 'href="http://e.example.org/e.png"', 'href="data:image/png;base64,AA"', 'href=""',
  'href="../up/f.PNG"', 'href="g.SVG?x"', 'href="h i.png"', 'href="%zz.png"', 'href="javascript:alert(1)"', 'href="\\\\back\\slash.png"',
  'type="image/svg+xml"', 'TYPE="IMAGE/SVG"', 'sizes="32x32"', 'sizes="16x16 64x64"', 'sizes="any"', 'sizes=" 31x31 "', 'hreflang="en"',
  'href = "spaced.png"', 'HREF="UP.png"'];
const genIcons = () => Array.from({ length: Math.floor(rnd() * 6) }, () => `<${pick(['link', 'LINK', 'link ', 'linkx'])} ${some(ICON_ATTRS, 4).join(' ')}${pick(['>', '/>', ''])}`).join(pick(['\n', '', ' ']));

const field = (name, value) => ({ name, value });
const genTask = () => {
  const keyed = pick(['ACME-12', 'DEMO-1', 'APP-123456', 'APP-1234567', 'NOPE-1', 12, null, undefined]);
  const text = () => (rnd() < 0.04 ? pick(['x'.repeat(301), '😀'.repeat(151)])
    : pick(['plain', '  spaced\tout\n', 'line\r\nbreak\rhere', 'esc \u001b[1m bold', 'bell\u0007', '', 5, null, undefined, 'a\u0085b']));
  const raw = {
    idReadable: keyed, summary: text(), description: rnd() < 0.01 ? 'd'.repeat(20481) : text(), resolved: pick([null, undefined, 0, 1, false]),
    updated: pick([1792321500123, 'x', null, Infinity, 1.5]), project: pick([{ shortName: text() }, null, 'ACME', [1]]),
    parent: pick([{ issues: [{ idReadable: pick(['ACME-1', 'X-1', 3]), summary: text() }] }, { issues: [] }, { issues: 'x' }, null, []]),
    subtasks: rnd() < 0.02 ? { issues: Array(52).fill({ idReadable: 'APP-1', summary: 's' }) } : pick([{ issues: Array.from({ length: Math.floor(rnd() * 4) }, () => ({ idReadable: pick(['ACME-2', 'NOPE-2', null]), summary: text(), resolved: pick([null, 1]), customFields: pick([[field('State', { name: text() })], 'x', null]) })) }, null]),
    customFields: pick([[field('Assignee', pick([{ login: pick(['jdoe', 5, '']), fullName: text(), name: text(), avatarUrl: pick(['/hub/x', null, 3]) }, null, 'str'])), field('Type', { name: text() }), field('State', 'x'), field('Priority', { name: text() })], 'x', null, [null, 1, field('State', { name: 'S' })]]),
    comments: pick([Array.from({ length: Math.floor(rnd() * 13) }, (_, i) => ({ author: pick([{ fullName: text(), login: text() }, null, 'a']), created: pick([i, 'x', null]), text: rnd() < 0.02 ? 'c'.repeat(4097) : text() })), null, 'x']),
  };
  return rnd() < 0.05 ? pick([null, 'x', [raw]]) : raw;
};
const genDiffFiles = () => (rnd() < 0.05 ? pick([null, 'x', {}]) : Array.from({ length: Math.floor(rnd() * 4) }, () => ({
  old_path: pick(['a.txt', 'dir/b', 7, undefined]), new_path: pick(['a.txt', 'c.txt', null]), new_file: pick([true, false, 1, undefined]),
  deleted_file: pick([true, false, '']), renamed_file: pick([true, false]), too_large: pick([true, undefined]), collapsed: pick([true, false]),
  diff: pick(['', '@@ -1 +1 @@\n-a\n+b\n', '--- a/x\n+++ b/x\n@@\n', '@@\n\n\n', 'no newline', undefined, null]),
})));
const genJsonl = () => some(['{"timestamp":"2026-10-18T10:00:00Z"}', '{"timestamp":1792317600000}', '{"timestamp":0}', '{"timestamp":-5}',
  '{"timestamp":"bad"}', 'null', '[]', '"s"', '{"x":1}', '  ', '', 'not json', '{"timestamp":"2026-10-18"}',
  '{"timestamp":1e400}', '\r', '{"timestamp":"2026-10-18T10:00:00.123+03:00"}\r'], 5).join('\n');
const genReflog = () => some(['abcdef0123456789abcdef0123456789abcdef01\x1fcommit: x\x1fHEAD@{1792320000}',
  'abcdef0123456789abcdef0123456789abcdef02\x1fcommit (amend): y\x1fHEAD@{1792320100}',
  'abcdef0123456789abcdef0123456789abcdef03\x1fcheckout: moving\x1fHEAD@{1792320200}',
  'abcdef0123456789abcdef0123456789abcdef04\x1frebase -i (pick): z\x1fHEAD@{1792310000}',
  'abcdef0123456789abcdef0123456789abcdef01\x1fcherry-pick: again\x1fHEAD@{1792320300}', 'ABCDEF0\x1fcommit: up\x1fHEAD@{1792320400}',
  'abcdef0\x1frevert: r\x1fHEAD@{1792320500}', 'abcdef0\x1fcommit: no selector\x1fHEAD', 'abcdef0\x1fmerge: m\x1fHEAD@{1792320600}', ''], 6).join('\n');

const REFS = ['agent:m1:tab-1', 'chat:12345678', 'agent:tab-2', 'agent:BAD REF', 'chat:short', 'agent:7', 'agent:x-y:z'];
const IDS = ['prj-abcdef', 'prj-second1', 'prj-zzzzzz', 'prj-x', 'nope'];
const genOp = () => {
  const op = pick(['project.create', 'project.update', 'project.delete', 'project.reorder', 'assign', 'pin', 'unpin', 'pins.reorder', 'forget', 'bad', undefined]);
  const o = { op };
  if (rnd() < 0.8) o.id = pick(IDS);
  if (rnd() < 0.6) o.name = pick(['Alpha', '  Spaced  name ', '', 'x'.repeat(61), 'x'.repeat(60), 5, '😀'.repeat(31)]);
  if (rnd() < 0.6) o.color = pick(['blue', 'green', 'teal', 'black', null]);
  if (rnd() < 0.3) o.index = pick([0, 1, 5, -1, 1.5, null, '1']);
  if (rnd() < 0.5) o.ids = pick([['prj-abcdef', 'prj-second1'], ['prj-second1', 'prj-abcdef'], ['prj-abcdef'], [1], 'x']);
  if (rnd() < 0.6) o.refs = pick([[pick(REFS)], [pick(REFS), pick(REFS)], [], 'x', REFS.slice(0, 3)]);
  if (rnd() < 0.5) o.ref = pick(REFS);
  if (rnd() < 0.5) o.projectId = pick([...IDS, null]);
  return o;
};

const clean = (v) => (v === undefined ? null : JSON.parse(JSON.stringify(v ?? null)));
const out = {};
const add = (name, input, fn) => {
  let output;
  try {
    output = { ok: clean(fn(input)) };
  } catch (e) {
    output = { throws: true };
  }
  (out[name] ??= []).push([input === undefined ? null : input, output]);
};
const BASE = 'https://icons.example.org/dir/page.html';
for (let i = 0; i < N; i++) {
  const u = genURL();
  add('url', u, (s) => {
    const p = new URL(s);
    return { protocol: p.protocol, username: p.username, password: p.password, hostname: p.hostname, port: p.port, pathname: p.pathname, search: p.search, href: p.href, origin: p.origin, k: p.searchParams.get('k') };
  });
  const rel = pick(['', '/', '//cdn.example.org/x', '?q', '#f', 'a/b', '../../c', '.', '..', 'https:x', 'http:y', 'https://h.example.org:443/p', '\\\\h2.example.org\\p']) + pick(['', '/z', '?a b', '#h']);
  add('resolve', rel, (s) => new URL(s, BASE).href);
  add('linkTarget', u, parseLinkTarget);
  add('gitlabRef', rnd() < 0.5 ? genRef() : u, (s) => ({ mr: mrRefOf(s), ref: gitlabRefOf(s) }));
  add('remote', genRemote(), gitlabProjectOf);
  add('artifactHtml', genHTML(), parseArtifactHtml);
  add('icons', genIcons(), (h) => iconCandidates(h, BASE));
  add('parentHost', pick(['a.b.c', 'b.c', 'c', 'x.y.z.w', '.a.b', 'a..b']), parentHost);
  add('task', genTask(), (raw) => parseTaskIssue(raw, (login, source) => `avatar:${login}:${source}`));
  add('taskText', rnd() < 0.01 ? pick(['x'.repeat(4097), '😀'.repeat(2049)]) : pick(['a\r\nb', '\u001b[0m\u0000\u009f\t', 7, 'exactly ten', '😀😀😀😀😀😀', '\r\n\r\r\n']), (t) => [cleanTaskText(t, 4096), cleanTaskText(t, 10)]);
  add('diff', genDiffFiles(), unifiedDiffOf);
  add('sessionStart', genJsonl(), parseSessionStart);
  add('reflog', genReflog(), (s) => parseReflogCommits(s, 1792320100000));
  const ops = rnd() < 0.1 ? pick([null, {}, { ops: 'x' }, { ops: [] }, { ops: Array(51).fill({ op: 'unpin', ref: 'agent:a' }) }]) : { ops: Array.from({ length: 1 + Math.floor(rnd() * 5) }, genOp) };
  add('organizer', ops, (body) => {
    const parsed = parseOrganizerOps(body);
    if (!parsed.ok) return { error: parsed.error };
    let current = emptyOrganizer();
    const first = applyOrganizerOps(current, [{ op: 'project.create', id: 'prj-abcdef', name: 'Alpha', color: 'blue' }, { op: 'pin', ref: 'agent:tab-2' }], 1000);
    current = first.organizer;
    const r = applyOrganizerOps(current, parsed.ops, 2000);
    return r.ok ? { organizer: r.organizer, changed: r.changed } : { error: r.error };
  });
  add('isOrganizer', pick([emptyOrganizer(), { version: 1, revision: 1.5 }, { version: 1, revision: 0, updatedAt: 'x', projects: [{ id: 'a', name: 'b', color: 'blue', createdAt: 1 }], assignments: { a: 'b' }, pins: ['agent:x'] },
    { version: 1, revision: 0, updatedAt: 'x', projects: [], assignments: [], pins: [] }, { version: '1' }, null, { version: 1, revision: 0, updatedAt: 'x', projects: [], assignments: { a: 1 }, pins: [] }]), isOrganizer);
}
const target = process.env.UNITS_OUT ?? path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'units.json');
fs.writeFileSync(target, JSON.stringify(out) + '\n');
console.log(`${Object.values(out).reduce((n, l) => n + l.length, 0)} cases → ${target}`);
