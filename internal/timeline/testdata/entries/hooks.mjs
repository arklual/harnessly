// Module hooks that load the previous runtime's modules straight from its TypeScript sources (node strips the types).
// Its own modules are real except the ones stubs.json names (stub files in this folder). An npm package is an inert
// stand-in unless stubs.json lists it under "real": then it is the installed one from OLD_RUNTIME_NODE_MODULES.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const here = path.dirname(fileURLToPath(import.meta.url));
const config = JSON.parse(fs.readFileSync(path.join(here, 'stubs.json'), 'utf8'));
// "{M}" in a stubbed module's name is the previous runtime's prefix of its own modules (OLD_RUNTIME_MODULE_PREFIX).
const STUBS = Object.fromEntries(Object.entries(config.stubs).map(([k, v]) => [k.replace('{M}', process.env.OLD_RUNTIME_MODULE_PREFIX ?? ''), v]));
const REAL = new Set(config.real ?? []);
const MODULES = process.env.OLD_RUNTIME_NODE_MODULES ?? '';

const tsFile = (base) => {
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

const stubURL = (name, context) => ({
  url: `stubfile:${encodeURIComponent(name)}?from=${encodeURIComponent(context.parentURL ? fileURLToPath(context.parentURL) : '')}`,
  shortCircuit: true,
});

const packageName = (specifier) => specifier.split('/').slice(0, specifier.startsWith('@') ? 2 : 1).join('/');

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (STUBS[specifier]) return stubURL(STUBS[specifier], context);
  if (specifier.startsWith('@/')) {
    const rel = specifier.slice(2);
    if (STUBS[rel]) return stubURL(STUBS[rel], context);
    const file = tsFile(path.join(SRC, rel));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
  } else if (specifier.startsWith('.')) {
    const parentFile = context.parentURL ? fileURLToPath(context.parentURL) : '';
    if (MODULES && parentFile.startsWith(MODULES)) return next(specifier, context);
    const parent = parentFile ? path.dirname(parentFile) : here;
    const file = tsFile(path.join(parent, specifier));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
    return next(specifier, context);
  } else if (specifier.startsWith('file:')) {
    return next(specifier, context);
  }
  const parentFile = context.parentURL ? fileURLToPath(context.parentURL) : '';
  if (MODULES && (REAL.has(packageName(specifier)) || parentFile.startsWith(MODULES))) {
    return next(specifier, { ...context, parentURL: pathToFileURL(path.join(MODULES, 'resolve-from.js')).href });
  }
  return { url: `stub:${encodeURIComponent(specifier)}?from=${encodeURIComponent(parentFile)}`, shortCircuit: true };
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
