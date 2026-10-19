// Module hooks that load the previous runtime's timeline socket straight from its TypeScript sources (node strips the
// types). Its own modules are real; tmux, processes, the status keeper, the layout store, the provider registry,
// logging and ids are the stubs in this folder. npm packages come from OLD_RUNTIME_MODULES (a node_modules folder of
// that runtime's release: ws, zod and diff must be real); any other is an inert stand-in.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const here = path.dirname(fileURLToPath(import.meta.url));
const MODULES = process.env.OLD_RUNTIME_MODULES ?? '';
const STUBS = {
  'lib/tmux': 'stub-tmux.mjs',
  'lib/status-manager': 'stub-status.mjs',
  'lib/layout-store': 'stub-layout.mjs',
  'lib/logger': 'stub-logger.mjs',
  'lib/process-utils': 'stub-process.mjs',
  'lib/preflight': 'stub-preflight.mjs',
  'lib/providers/codex/preflight': 'stub-preflight.mjs',
  'lib/providers': 'stub-providers.mjs',
  'lib/providers/index': 'stub-providers.mjs',
  'lib/providers/claude': 'stub-provider-ids.mjs',
  'lib/providers/codex': 'stub-provider-ids.mjs',
  'lib/agent-session-recovery': 'stub-recovery.mjs',
  nanoid: 'stub-nanoid.mjs',
};
const REAL = new Set(['ws', 'zod', 'diff']);
// Modules named after the product are matched by the rest of their name.
const SUFFIX_STUBS = [['-launch-options-server', 'stub-launch.mjs']];
const stubOf = (rel) => STUBS[rel] ?? SUFFIX_STUBS.find(([suffix]) => rel.startsWith('lib/') && rel.endsWith(suffix))?.[1];

const tsFile = (base) => {
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

// A stub answers for the names its file exports; any other name the importer takes from it is an inert stand-in.
const stubURL = (name, context) => ({
  url: `stubfile:${encodeURIComponent(name)}?from=${encodeURIComponent(context.parentURL ? fileURLToPath(context.parentURL) : '')}`,
  shortCircuit: true,
});

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (STUBS[specifier]) return stubURL(STUBS[specifier], context);
  if (MODULES && context.parentURL?.startsWith('file:') && fs.realpathSync(fileURLToPath(context.parentURL)).startsWith(fs.realpathSync(MODULES))) {
    return next(specifier, context);
  }
  if (REAL.has(specifier) && MODULES) return next(specifier, { ...context, parentURL: pathToFileURL(path.join(MODULES, 'x.js')).href });
  if (specifier.startsWith('@/')) {
    const rel = specifier.slice(2);
    if (stubOf(rel)) return stubURL(stubOf(rel), context);
    const file = tsFile(path.join(SRC, rel));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
  } else if (specifier.startsWith('.')) {
    const parent = context.parentURL ? path.dirname(fileURLToPath(context.parentURL)) : here;
    const rel = path.relative(SRC, path.join(parent, specifier));
    if (stubOf(rel)) return stubURL(stubOf(rel), context);
    const file = tsFile(path.join(parent, specifier));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
    return next(specifier, context);
  } else if (specifier.startsWith('file:')) {
    return next(specifier, context);
  }
  const from = context.parentURL ? fileURLToPath(context.parentURL) : '';
  return { url: `stub:${encodeURIComponent(specifier)}?from=${encodeURIComponent(from)}`, shortCircuit: true };
}

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

const INERT = [
  'const handler = { get: (_, k) => (k === "then" ? undefined : k === Symbol.iterator ? function* () {} : k === Symbol.toPrimitive ? () => "stub" : any), apply: () => any, construct: () => any };',
  'const any = new Proxy(function () {}, handler);',
];

const requested = (u) => {
  const from = u.searchParams.get('from');
  return from && fs.existsSync(from) ? fs.readFileSync(from, 'utf8') : '';
};

export async function load(url, context, next) {
  if (url.startsWith('stubfile:')) {
    const u = new URL(url);
    const file = path.join(here, decodeURIComponent(u.pathname));
    const own = new Set([...fs.readFileSync(file, 'utf8').matchAll(/^export (?:const|let|function|async function) (\w+)/gm)].map((m) => m[1]));
    const source = requested(u);
    // Every name the importer imports from anywhere: one it takes from this stub but the stub lacks is inert.
    const wanted = [...source.matchAll(/import\s+(?!type\s)\{([^}]*)\}\s+from\s+['"]([^'"]+)['"]/g)]
      .flatMap((m) => m[1].split(',').map((p) => p.trim()).filter((p) => p && !p.startsWith('type ')).map((p) => p.split(/\s+as\s+/)[0].trim()));
    const extra = [...new Set(wanted)].filter((n) => !own.has(n));
    return {
      format: 'module',
      source: [`export * from ${JSON.stringify(pathToFileURL(file).href)};`, ...INERT, ...extra.map((n) => `export const ${n} = any;`)].join('\n'),
      shortCircuit: true,
    };
  }
  if (!url.startsWith('stub:')) return next(url, context);
  const u = new URL(url);
  const specifier = decodeURIComponent(u.pathname);
  const names = importedNames(requested(u), specifier);
  return { format: 'module', source: [...INERT, 'export default any;', ...names.map((n) => `export const ${n} = any;`)].join('\n'), shortCircuit: true };
}
