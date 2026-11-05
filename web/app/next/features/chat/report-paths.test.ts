import { describe, expect, it } from 'vitest';
import { fileNameOf, pathsOf, withFileNames } from '@/next/features/chat/report-paths';

describe('report paths', () => {
  it('names a file by its last part, without the line', () => {
    expect(fileNameOf('/tmp/claude-995/x/scratchpad/research/inventory.md')).toBe('inventory.md');
    expect(fileNameOf('web/app/x.ts:12')).toBe('x.ts');
  });

  it('finds the files a report names once each, in order, and skips plain code', () => {
    const md = 'See `/a/b/one.md`, then `web/two.ts:3` and `/a/b/one.md` again; run `pnpm test`.';
    expect(pathsOf(md)).toEqual(['/a/b/one.md', 'web/two.ts:3']);
  });

  it('cuts the paths of a report to file names for its folded line', () => {
    expect(withFileNames('Отчёт: `/tmp/x/research/perf.md`. Run `pnpm test`.')).toBe(
      'Отчёт: `perf.md`. Run `pnpm test`.',
    );
  });
});
