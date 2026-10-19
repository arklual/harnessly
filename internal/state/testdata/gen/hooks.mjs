// Module hooks that load the previous runtime's layout and workspace stores straight from its TypeScript sources (node
// strips the types): the stores and the pure modules they use are real, tmux, the sync socket and ids are the stubs in
// this folder, and anything else is an inert stand-in.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const here = path.dirname(fileURLToPath(import.meta.url));
const REAL = new Set([
  'lib/layout-store', 'lib/workspace-store', 'lib/layout-tree', 'lib/layout-ops', 'lib/workspace-order',
  'lib/workspaces-view', 'lib/cluster-state-client', 'lib/data-dir', 'lib/tab-name', 'lib/tab-title',
  'lib/agent-session-id', 'lib/providers/claude', 'lib/providers/codex',
]);
const STUBS = { 'lib/tmux': 'stub-tmux.mjs', 'lib/sync-server': 'stub-sync.mjs', nanoid: 'stub-nanoid.mjs' };

const tsFile = (rel) => {
  const base = path.join(SRC, rel);
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (STUBS[specifier]) return { url: pathToFileURL(path.join(here, STUBS[specifier])).href, shortCircuit: true };
  if (specifier.startsWith('@/')) {
    const rel = specifier.slice(2);
    if (STUBS[rel]) return { url: pathToFileURL(path.join(here, STUBS[rel])).href, shortCircuit: true };
    const file = tsFile(rel);
    if (file && (REAL.has(rel) || rel.startsWith('types/'))) return { url: pathToFileURL(file).href, shortCircuit: true };
  } else if (specifier.startsWith('.') || specifier.startsWith('file:')) {
    return next(specifier, context);
  }
  const from = context.parentURL ? fileURLToPath(context.parentURL) : '';
  return { url: `stub:${encodeURIComponent(specifier)}?from=${encodeURIComponent(from)}`, shortCircuit: true };
}

// The names the importing file takes from a module, so the stand-in can export each of them.
const importedNames = (source, specifier) => {
  const names = new Set();
  const esc = specifier.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
  const re = new RegExp(`import\\s+(?!type\\s)([^;]*?)\\s+from\\s+['"]${esc}['"]`, 'g');
  for (const m of source.matchAll(re)) {
    const braces = m[1].match(/\{([^}]*)\}/);
    if (braces) {
      for (const part of braces[1].split(',')) {
        const p = part.trim();
        if (!p || p.startsWith('type ')) continue;
        names.add(p.split(/\s+as\s+/)[0].trim());
      }
    }
  }
  return [...names];
};

export async function load(url, context, next) {
  if (!url.startsWith('stub:')) return next(url, context);
  const u = new URL(url);
  const specifier = decodeURIComponent(u.pathname);
  const from = u.searchParams.get('from');
  const names = from && fs.existsSync(from) ? importedNames(fs.readFileSync(from, 'utf8'), specifier) : [];
  const source = [
    'const handler = { get: (_, k) => (k === "then" ? undefined : k === Symbol.iterator ? function* () {} : k === Symbol.toPrimitive ? () => "stub" : any), apply: () => any };',
    'const any = new Proxy(function () {}, handler);',
    'export default any;',
    ...names.map((n) => `export const ${n} = any;`),
  ].join('\n');
  return { format: 'module', source, shortCircuit: true };
}
