// Module hooks that load the previous runtime's Codex parser straight from its TypeScript sources (node strips the
// types): `@/` is OLD_RUNTIME_SRC/src, bare packages come from the node_modules folder OLD_RUNTIME_MODULES (zod must be
// the real one), and the logger, the data folder, the daily report and nanoid are the stubs in this folder.
import fs from 'node:fs';
import path from 'node:path';
import { builtinModules } from 'node:module';
import { pathToFileURL, fileURLToPath } from 'node:url';

const SRC = path.join(process.env.OLD_RUNTIME_SRC ?? '', 'src');
const MODULES = process.env.OLD_RUNTIME_MODULES ?? '';
const here = path.dirname(fileURLToPath(import.meta.url));
const STUBS = {
  'lib/logger': 'stub-logger.mjs',
  'lib/data-dir': 'stub-data-dir.mjs',
  'lib/stats/daily-report-builder': 'stub-daily-report.mjs',
};

const tsFile = (base) => {
  if (fs.existsSync(base + '.ts')) return base + '.ts';
  if (fs.existsSync(path.join(base, 'index.ts'))) return path.join(base, 'index.ts');
  return null;
};

const fromSrc = (file) => {
  const rel = path.relative(SRC, file).replace(/\.ts$/, '');
  if (STUBS[rel]) return { url: pathToFileURL(path.join(here, STUBS[rel])).href, shortCircuit: true };
  return { url: pathToFileURL(file).href, shortCircuit: true };
};

export async function resolve(specifier, context, next) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return next(specifier, context);
  if (specifier === 'nanoid') return { url: pathToFileURL(path.join(here, 'stub-nanoid.mjs')).href, shortCircuit: true };
  if (specifier.startsWith('@/')) {
    const file = tsFile(path.join(SRC, specifier.slice(2)));
    if (file) return fromSrc(file);
  } else if (specifier.startsWith('.') && context.parentURL?.startsWith('file:')) {
    const parent = path.dirname(fileURLToPath(context.parentURL));
    const file = tsFile(path.join(parent, specifier));
    if (file) return fromSrc(file);
    return next(specifier, context);
  } else if (!specifier.startsWith('file:')) {
    return next(specifier, { ...context, parentURL: pathToFileURL(path.join(MODULES, 'index.js')).href });
  }
  return next(specifier, context);
}
