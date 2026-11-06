#!/usr/bin/env node
// Checks that every server route the web code calls exists in the public Hub or runtime.
//
//   node tools/web-routes-check.mjs            fail (exit 1) on a call with no server part and no known-gap entry
//   node tools/web-routes-check.mjs --report   print the table of every call without a public route (Markdown)
//   node tools/web-routes-check.mjs --json     the same as JSON
//
// How it works. The web side is every string literal in web/app that starts with `/api/` (a template's `${…}` is a
// wildcard segment). The server side is every route the Go code registers: Hub mux patterns (`"GET /api/…"`), and the
// runtime's `platformAPI + "/…"` / `"/api/…"` literals (`platformAPI` stands for /api/platform). A call is served
// when a route equals it or is a segment prefix of it. A call that nothing serves must be listed in
// tools/web-routes-known.json with the capability it hangs on (GET /api/capabilities) and the task that closes the gap;
// an entry that is served after all, or that nothing calls any more, fails the check too, so the list stays honest.
//
// Limits: the server side is an over-approximation (a literal in Go counts as a route), so a call can be missed, never
// invented; calls built from several variables (`${a}${b}`) are checked by their static head only.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));
const PLATFORM = '/api/platform';
const KNOWN_FILE = join(ROOT, 'tools', 'web-routes-known.json');

const walk = (dir, accept, skip = () => false) =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (skip(e.name, p)) return [];
    if (e.isDirectory()) return walk(p, accept, skip);
    return accept(e.name) ? [p] : [];
  });

const lineOf = (text, index) => text.slice(0, index).split('\n').length;

/** `/api/a/${x}/b?q=1` → ['api','a','*','b']; a `${…}` that is part of a segment ends the path (a dynamic tail). */
export function segments(raw) {
  const path = raw.split(/[?#]/)[0];
  const out = [];
  for (const part of path.split('/').slice(1)) {
    if (part === '') continue;
    if (/^\$\{[^}]*\}$/.test(part) || /^\{[^}]*\}$/.test(part) || part === '*') {
      out.push('*');
    } else if (part.includes('${')) {
      const head = part.slice(0, part.indexOf('${'));
      if (head) out.push(head);
      out.push('**');
      break;
    } else {
      out.push(part);
    }
  }
  return out;
}

const isWild = (s) => s === '*' || (s.startsWith('{') && s.endsWith('}'));

/** The route (server) serves the call (web) when it equals it or is a segment prefix of it; `*` matches one segment.
 *  A call that ends in `**` (a dynamic tail) is served as well by a longer route that starts like its head. */
export function serves(route, call) {
  const dynamic = call[call.length - 1] === '**';
  const head = dynamic ? call.slice(0, -1) : call;
  if (route.length > head.length && !dynamic) return false;
  const n = Math.min(route.length, head.length);
  for (let i = 0; i < n; i++) {
    if (isWild(route[i]) || head[i] === '*') continue;
    if (route[i] !== head[i]) return false;
  }
  return true;
}

export function webCalls(appDir) {
  const files = walk(
    appDir,
    (n) => /\.tsx?$/.test(n) && !/\.(test|spec)\.tsx?$/.test(n),
    (n) => n === 'test' || n === 'node_modules',
  );
  const re = /(['"`])(\/(?:agents\/)?api(?:\/[^'"`\s]*)?)\1?/g;
  const calls = [];
  for (const file of files) {
    const text = readFileSync(file, 'utf8');
    const lines = text.split('\n');
    for (const m of text.matchAll(re)) {
      const at = lineOf(text, m.index);
      const line = lines[at - 1].trim();
      if (/^(\*|\/\/|\/\*)/.test(line)) continue;
      // A literal that stops at a quote is the whole path; a template literal can contain quotes inside `${…}`.
      let lit = m[2];
      const close = lit.search(/['"`]/);
      if (close >= 0) lit = lit.slice(0, close);
      // The bare prefix is configuration (the default of platformApi()), not a call.
      if (lit === '/api' || lit === '/api/' || lit === PLATFORM) continue;
      const segs = segments(lit.replace(/^\/agents/, ''));
      calls.push({ path: '/' + segs.join('/'), segs, file: relative(ROOT, file), line: at });
    }
  }
  // The helpers that add the prefix themselves: platformPath('/x'), gatewayPath('/x') → /api/platform/x, clusterApi('/x') → /api/v2/x.
  const helper = /\b(platformPath|gatewayPath|clusterApi)\(\s*(['"`])(\/[^'"`]*)/g;
  const prefixOf = { platformPath: PLATFORM, gatewayPath: PLATFORM, clusterApi: '/api/v2' };
  for (const file of files) {
    const text = readFileSync(file, 'utf8');
    for (const m of text.matchAll(helper)) {
      const at = lineOf(text, m.index);
      const segs = segments(prefixOf[m[1]] + m[3]);
      calls.push({ path: '/' + segs.join('/'), segs, file: relative(ROOT, file), line: at });
    }
  }
  return calls;
}

export function serverRoutes() {
  const files = walk(
    join(ROOT, 'cmd'),
    (n) => n.endsWith('.go') && !n.endsWith('_test.go'),
  ).concat(
    walk(
      join(ROOT, 'internal'),
      (n) => n.endsWith('.go') && !n.endsWith('_test.go'),
      (n) => n === 'testdata',
    ),
  );
  // Lists of paths that are not handlers: the previous runtime's route table and the gateway's filter.
  const NOT_HANDLERS = new Set(['cmd/runtime/oldroutes.go', 'internal/agent/routefilter.go']);
  const routes = new Map();
  const add = (path, file) => {
    const segs = segments(path.replace(/\{(\w+)\.\.\.\}/g, '{$1}'));
    const key = segs.join('/');
    // `/api` or `/api/platform` alone is a prefix the code strips or compares, not a route that serves what is under it.
    if (segs.length < 3 && segs[1] === 'platform') return;
    if (segs.length < 2) return;
    if (!routes.has(key)) routes.set(key, { segs, file });
  };
  for (const file of files) {
    const text = readFileSync(file, 'utf8');
    const rel = relative(ROOT, file);
    if (NOT_HANDLERS.has(rel)) continue;
    // Hub mux patterns and plain literals: "GET /api/x/{id}", "/api/x".
    for (const m of text.matchAll(/"(?:[A-Z]+ )?(\/api\/[A-Za-z0-9_\-./{}:]*)"/g)) add(m[1], rel);
    // The runtime's platform routes: platformAPI + "/x", PlatformAPI+"/x", and the prefix-literals built from it.
    // A file that works with the platform prefix names it differently (`p`, `prefix`, `base`): `x + "/route"` counts there.
    if (/[pP]latformAPI/.test(text)) {
      for (const m of text.matchAll(/\b[A-Za-z_][A-Za-z0-9_.]*\s*\+\s*"(\/[A-Za-z0-9_\-./{}:]*)"/g)) add(PLATFORM + m[1], rel);
      // A table of routes walked in a loop and joined to the prefix there: {"/x", handler}.
      for (const m of text.matchAll(/\{\s*"(\/[A-Za-z0-9_\-./{}:]*)"\s*,/g)) add(PLATFORM + m[1], rel);
    }
    for (const m of text.matchAll(/[pP]latformAPI\s*\+\s*"(\/[A-Za-z0-9_\-./{}:]*)"/g)) add(PLATFORM + m[1], rel);
  }
  return [...routes.values()];
}

const matchEntry = (entry, call) => {
  const pat = segments(entry.path);
  return serves(pat, call.segs) || serves(call.segs, pat);
};

export function analyse() {
  const routes = serverRoutes();
  const calls = webCalls(join(ROOT, 'web', 'app'));
  const known = JSON.parse(readFileSync(KNOWN_FILE, 'utf8')).known;

  const unserved = calls.filter((c) => !routes.some((r) => serves(r.segs, c.segs)));
  const byPath = new Map();
  for (const c of unserved) {
    const e = byPath.get(c.path) ?? { path: c.path, where: [] };
    e.where.push(`${c.file}:${c.line}`);
    byPath.set(c.path, e);
  }
  const gaps = [...byPath.values()].sort((a, b) => a.path.localeCompare(b.path));

  const covered = (g) => known.find((k) => matchEntry(k, { segs: segments(g.path) }));
  const newGaps = gaps.filter((g) => !covered(g));
  const staleKnown = known.filter((k) => {
    const pat = segments(k.path);
    const stillCalled = unserved.some((c) => matchEntry(k, c));
    const nowServed = routes.some((r) => serves(r.segs, pat));
    return !stillCalled || nowServed;
  });
  const incomplete = known.filter((k) => !k.capability || !k.task || !k.note);
  // The capability an entry hangs on must be one GET /api/capabilities names (the runtime's list).
  const declared = readFileSync(join(ROOT, 'cmd', 'runtime', 'routes_capabilities.go'), 'utf8');
  const unknownCapability = known.filter((k) => k.capability && !declared.includes(`"${k.capability}"`));
  return { calls, routes, gaps, known, newGaps, staleKnown, incomplete, unknownCapability, covered };
}

function report(a) {
  const rows = a.gaps.map((g) => {
    const k = a.covered(g);
    const where = [...new Set(g.where)].slice(0, 3).join(', ') + (g.where.length > 3 ? ', …' : '');
    return `| \`${g.path}\` | ${k ? k.capability : '**none**'} | ${k ? k.task : '—'} | ${where} |`;
  });
  return ['| Path the web calls | Capability | Task | Called from |', '|---|---|---|---|', ...rows].join('\n');
}

function main() {
  const args = process.argv.slice(2);
  const a = analyse();
  if (args.includes('--report')) {
    console.log(report(a));
    return 0;
  }
  if (args.includes('--json')) {
    console.log(JSON.stringify({ gaps: a.gaps, known: a.known }, null, 2));
    return 0;
  }
  let bad = 0;
  if (a.newGaps.length) {
    bad = 1;
    console.error('The web calls routes the public hub and runtime do not serve:\n');
    for (const g of a.newGaps) console.error(`  ${g.path}\n    ${[...new Set(g.where)].join('\n    ')}`);
    console.error(
      '\nAdd the server part, or (if the call is bound to a capability that may be off) list the path in\n' +
        'tools/web-routes-known.json with the capability of GET /api/capabilities and the task that closes the gap.',
    );
  }
  if (a.staleKnown.length) {
    bad = 1;
    console.error('\nEntries of tools/web-routes-known.json that no longer describe a gap (served now, or not called):');
    for (const k of a.staleKnown) console.error(`  ${k.path}  (${k.task})`);
  }
  if (a.incomplete.length) {
    bad = 1;
    console.error('\nEntries without capability, task or note:');
    for (const k of a.incomplete) console.error(`  ${k.path}`);
  }
  if (a.unknownCapability.length) {
    bad = 1;
    console.error('\nEntries whose capability GET /api/capabilities (cmd/runtime/routes_capabilities.go) does not name:');
    for (const k of a.unknownCapability) console.error(`  ${k.path}  → ${k.capability}`);
  }
  if (!bad) {
    console.log(
      `web-routes: ${a.calls.length} calls, ${a.routes.length} server routes, ${a.gaps.length} known gaps, nothing new.`,
    );
  }
  return bad;
}

if (process.argv[1] && statSync(process.argv[1]).ino === statSync(fileURLToPath(import.meta.url)).ino) {
  process.exit(main());
}
