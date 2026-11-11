import { describe, expect, it } from 'vitest';
import { FILE_RANK, nextUnviewed, neighbour, rankOf, reviewOrder } from '@/next/features/review/app/order';
import { FILE_STATUS, type IReviewFile } from '@/next/features/review/model';

const f = (path: string): IReviewFile => ({
  path,
  oldPath: path,
  status: FILE_STATUS.modified,
  additions: 1,
  deletions: 0,
  binary: false,
  patch: '',
  version: path,
});

describe('rankOf', () => {
  it('knows tests, lock files and code', () => {
    expect(rankOf('internal/x/registry_test.go')).toBe(FILE_RANK.test);
    expect(rankOf('web/app/a.test.tsx')).toBe(FILE_RANK.test);
    expect(rankOf('web/e2e/tour.ts')).toBe(FILE_RANK.test);
    expect(rankOf('web/pnpm-lock.yaml')).toBe(FILE_RANK.lock);
    expect(rankOf('go.sum')).toBe(FILE_RANK.lock);
    expect(rankOf('web/app/surface.gen.ts')).toBe(FILE_RANK.lock);
    expect(rankOf('internal/x/registry.go')).toBe(FILE_RANK.code);
    expect(rankOf('src/contest.ts')).toBe(FILE_RANK.code);
  });
});

describe('reviewOrder', () => {
  it('puts tests first and locks last, keeping the order within each', () => {
    const files = [f('go.sum'), f('a.go'), f('a_test.go'), f('b.go'), f('b_test.go')];
    expect(reviewOrder(files).map((x) => x.path)).toEqual([
      'a_test.go',
      'b_test.go',
      'a.go',
      'b.go',
      'go.sum',
    ]);
  });
});

describe('nextUnviewed', () => {
  const order = [f('a'), f('b'), f('c')];
  it('goes to the next file not viewed, round to the start', () => {
    expect(nextUnviewed(order, 'a', (x) => x.path === 'b')?.path).toBe('c');
    expect(nextUnviewed(order, 'c', () => false)?.path).toBe('a');
  });
  it('is null when every other file is viewed', () => {
    expect(nextUnviewed(order, 'a', (x) => x.path !== 'a')).toBeNull();
  });
});

describe('neighbour', () => {
  const order = [f('a'), f('b')];
  it('steps and stops at the ends', () => {
    expect(neighbour(order, 'a', 1)?.path).toBe('b');
    expect(neighbour(order, 'a', -1)).toBeNull();
    expect(neighbour(order, null, 1)?.path).toBe('a');
  });
});
