import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { describe, expect, it, vi } from 'vitest';

// An installation that serves the platform API under its own prefix (RUNTIME_PLATFORM_API) and the page at /agents.
vi.mock('@/platform/config', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/platform/config')>();
  return { ...real, appBasePath: () => '/agents', platformApi: () => '/api/custom' };
});

const { prefixAppPath } = await import('@/lib/app-path');

const APP_DIR = join(__dirname, '..');

const sources = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === 'test' ? [] : sources(path);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : [];
  });

describe('platform API paths', () => {
  it('follow the installation prefix and the page base', () => {
    expect(prefixAppPath('/api/platform/conversations')).toBe('/agents/api/custom/conversations');
    expect(prefixAppPath('/api/platform/search?q=x')).toBe('/agents/api/custom/search?q=x');
    expect(prefixAppPath('/api/other')).toBe('/agents/api/other');
  });

  it('are never written with the page base in the code', () => {
    // A literal `/agents/api/…` or `${APP_BASE_PATH}/api/…` skips both the prefix and a custom base path.
    const offenders = sources(APP_DIR).flatMap((file) =>
      readFileSync(file, 'utf8')
        .split('\n')
        .map((line, i) => ({ line: line.trim(), at: `${relative(APP_DIR, file)}:${i + 1}` }))
        .filter(({ line }) => !/^(\*|\/\/|\/\*)/.test(line))
        .filter(({ line }) => /['"`]\/agents\/api\/|APP_BASE_PATH\}\/api\//.test(line))
        .map(({ at }) => at),
    );
    expect(offenders).toEqual([]);
  });
});
