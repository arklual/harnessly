// Module hooks that load the previous runtime's preview modules straight from its TypeScript sources (node strips the
// types). Its own modules are real, apart from the few stubbed in this folder (tmux, the data folder, the logger, the port scan);
// npm packages come from NODE_MODULES (a checkout's installed packages), so zod and jose are the real ones.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const MODULES = process.env.NODE_MODULES ?? '';
const here = path.dirname(fileURLToPath(import.meta.url));
const STUBS = {
  'lib/tmux': 'stub-tmux.mjs',
  'lib/layout-store': 'stub-layout-store.mjs',
  'lib/data-dir': 'stub-data-dir.mjs',
  'lib/logger': 'stub-logger.mjs',
  [`lib/${process.env.PRODUCT}-preview/ports-scan`]: 'stub-ports-scan.mjs',
};

const tsFile = (base) => {
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (specifier.startsWith('@/')) {
    const rel = specifier.slice(2);
    if (STUBS[rel]) return { url: pathToFileURL(path.join(here, STUBS[rel])).href, shortCircuit: true };
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
  return next(specifier, { ...context, parentURL: pathToFileURL(path.join(MODULES, 'x.js')).href });
}
