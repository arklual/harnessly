#!/usr/bin/env node
// The page's contract with the server, written next to a built page (web/dist/web-release.json), so a page
// built from one tree is served by a runtime of another only when that runtime answers every call the page makes.
//
//   node tools/web-compat.mjs manifest [--out web/dist/web-release.json] [--version X]
//       routes: every server route of this tree (tools/web-routes-check.mjs: the Go sources)
//       calls:  every call of the page (web/app) that this tree serves; the known gaps (tools/web-routes-known.json)
//               are left out: the page asks GET /api/capabilities before it makes them
//   node tools/web-compat.mjs check <page web-release.json> <runtime web-release.json>
//       prints the page's calls the runtime does not serve; exit 1 when there is one
//
// The runtime does the same check itself (internal/webrelease) before it serves a page from RUNTIME_WEB_DIR: the
// calls of that page against the routes of the page compiled into it, which came from its own tree.
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { serverRoutes, serves, webCalls } from './web-routes-check.mjs';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));

const uniq = (list) => {
  const seen = new Map();
  for (const segs of list) seen.set(segs.join('/'), segs);
  return [...seen.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([, segs]) => segs);
};

export function manifest(version = '') {
  const routes = serverRoutes().map((r) => r.segs);
  const calls = webCalls(join(ROOT, 'web', 'app'))
    .map((c) => c.segs)
    .filter((segs) => routes.some((r) => serves(r, segs)));
  let sourceSha = process.env.CI_COMMIT_SHA ?? '';
  if (!sourceSha) {
    try {
      sourceSha = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: ROOT, encoding: 'utf8' }).trim();
    } catch {
      sourceSha = '';
    }
  }
  return { schema: 1, version, sourceSha, calls: uniq(calls), routes: uniq(routes) };
}

/** The page's calls that none of the runtime's routes serves. */
export function missing(page, runtime) {
  return page.calls.filter((c) => !runtime.routes.some((r) => serves(r, c))).map((c) => '/' + c.join('/'));
}

function main(args) {
  const [cmd, ...rest] = args;
  const flag = (name) => {
    const i = rest.indexOf(name);
    return i >= 0 ? rest[i + 1] : undefined;
  };
  if (cmd === 'manifest') {
    const m = manifest(flag('--version') ?? '');
    const out = flag('--out');
    const text = JSON.stringify(m) + '\n';
    if (!out) {
      process.stdout.write(text);
      return 0;
    }
    mkdirSync(dirname(out), { recursive: true });
    writeFileSync(out, text);
    console.log(`web-compat: ${m.calls.length} calls, ${m.routes.length} routes -> ${out}`);
    return 0;
  }
  if (cmd === 'check' && rest.length === 2) {
    const [page, runtime] = rest.map((f) => JSON.parse(readFileSync(f, 'utf8')));
    const gaps = missing(page, runtime);
    for (const g of gaps) console.log(`not served: ${g}`);
    console.log(`web-compat: ${page.calls.length - gaps.length} of ${page.calls.length} calls served`);
    return gaps.length ? 1 : 0;
  }
  console.error('usage: web-compat.mjs manifest [--out FILE] [--version X] | check <page.json> <runtime.json>');
  return 2;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(main(process.argv.slice(2)));
}
