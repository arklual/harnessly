// Module hooks that load the previous runtime's transcript parser straight from its TypeScript sources (node strips
// the types). Its own modules are real; zod and diff are the real packages, resolved from OLD_RUNTIME_MODULES (a
// node_modules folder of that runtime's release); nanoid is the counter in stub-nanoid.mjs; any other npm package is an
// inert stand-in.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const MODULES = process.env.OLD_RUNTIME_MODULES ?? '';
const REAL = new Set(['zod', 'diff']);
const here = path.dirname(fileURLToPath(import.meta.url));

const tsFile = (base) => {
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (specifier === 'nanoid') return { url: pathToFileURL(path.join(here, 'stub-nanoid.mjs')).href, shortCircuit: true };
  const pkg = specifier.startsWith('@') ? specifier.split('/').slice(0, 2).join('/') : specifier.split('/')[0];
  if (REAL.has(pkg)) return next(specifier, { ...context, parentURL: pathToFileURL(path.join(MODULES, 'resolve-from.mjs')).href });
  if (context.parentURL?.startsWith(pathToFileURL(MODULES).href)) return next(specifier, context);
  if (specifier.startsWith('@/')) {
    const file = tsFile(path.join(SRC, specifier.slice(2)));
    if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
  } else if (specifier.startsWith('.') || specifier.startsWith('file:')) {
    if (specifier.startsWith('.') && context.parentURL?.startsWith(pathToFileURL(SRC).href)) {
      const file = tsFile(path.join(path.dirname(fileURLToPath(context.parentURL)), specifier));
      if (file) return { url: pathToFileURL(file).href, shortCircuit: true };
    }
    return next(specifier, context);
  }
  const from = context.parentURL ? fileURLToPath(context.parentURL) : '';
  return { url: `stub:${encodeURIComponent(specifier)}?from=${encodeURIComponent(from)}`, shortCircuit: true };
}

const importedNames = (source, specifier) => {
  const names = new Set();
  const esc = specifier.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
  for (const m of source.matchAll(new RegExp(`import\\s+(?!type\\s)([^;]*?)\\s+from\\s+['"]${esc}['"]`, 'g'))) {
    const braces = m[1].match(/\{([^}]*)\}/);
    for (const part of braces ? braces[1].split(',') : []) {
      const p = part.trim();
      if (p && !p.startsWith('type ')) names.add(p.split(/\s+as\s+/)[0].trim());
    }
  }
  return [...names];
};

const INERT = [
  'const handler = { get: (_, k) => (k === "then" ? undefined : k === Symbol.iterator ? function* () {} : k === Symbol.toPrimitive ? () => "stub" : any), apply: () => any, construct: () => any };',
  'const any = new Proxy(function () {}, handler);',
];

export async function load(url, context, next) {
  if (!url.startsWith('stub:')) return next(url, context);
  const u = new URL(url);
  const from = u.searchParams.get('from');
  const source = from && fs.existsSync(from) ? fs.readFileSync(from, 'utf8') : '';
  const names = importedNames(source, decodeURIComponent(u.pathname));
  return { format: 'module', source: [...INERT, 'export default any;', ...names.map((n) => `export const ${n} = any;`)].join('\n'), shortCircuit: true };
}
