// Module hooks that load the previous runtime's API handlers straight from its TypeScript sources (node strips the
// types). Its own modules are real; tmux, the sync socket, the status keeper, sign-in, logging, agent availability and
// ids are the stubs in this folder; an npm package (none is installed) is an inert stand-in.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
// GOLDEN_STUB_DIR names another folder of stubs, with its stubs.json mapping modules to them (another golden's set).
const here = process.env.GOLDEN_STUB_DIR || path.dirname(fileURLToPath(import.meta.url));
// A ${NAME} in a module name of stubs.json is that environment variable (the module names a product).
const OWN_STUBS = fs.existsSync(path.join(here, 'stubs.json'))
  ? Object.fromEntries(Object.entries(JSON.parse(fs.readFileSync(path.join(here, 'stubs.json'), 'utf8')))
    .map(([k, v]) => [k.replace(/\$\{(\w+)\}/g, (_, n) => process.env[n] ?? ''), v]))
  : null;
const STUBS = OWN_STUBS ?? {
  'lib/tmux': 'stub-tmux.mjs',
  'lib/sync-server': 'stub-sync.mjs',
  'lib/status-manager': 'stub-status.mjs',
  'lib/auth': 'stub-auth.mjs',
  'lib/logger': 'stub-logger.mjs',
  'lib/agent-availability': 'stub-availability.mjs',
  nanoid: 'stub-nanoid.mjs',
};

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
  if (specifier.startsWith('@/')) {
    const rel = specifier.slice(2);
    if (STUBS[rel]) return stubURL(STUBS[rel], context);
    const file = tsFile(path.join(SRC, rel));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
  } else if (specifier.startsWith('.')) {
    const parent = context.parentURL ? path.dirname(fileURLToPath(context.parentURL)) : here;
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
