import { describe, expect, it } from 'vitest';
import { autoExpandTree, isHeavyContent, isHeavyFile, REVIEW_LIMITS } from '@/next/features/review/limits';
import { FILE_STATUS, type IReviewFile } from '@/next/features/review/model';

const file = (additions: number, deletions: number, patch = ''): IReviewFile => ({
  path: 'a.ts',
  oldPath: 'a.ts',
  status: FILE_STATUS.modified,
  additions,
  deletions,
  binary: false,
  patch,
  version: 'v',
});

describe('the limits of a heavy diff', () => {
  it('a file over 5000 changed lines waits for «show anyway»', () => {
    expect(isHeavyFile(file(2500, 2500))).toBe(false);
    expect(isHeavyFile(file(2500, 2501))).toBe(true);
  });

  it('a patch or a version over 1 MB does too', () => {
    expect(isHeavyFile(file(1, 1, 'x'.repeat(REVIEW_LIMITS.fileBytes + 1)))).toBe(true);
    expect(isHeavyContent({ before: '', after: 'x'.repeat(REVIEW_LIMITS.fileBytes), complete: true })).toBe(
      false,
    );
    expect(
      isHeavyContent({ before: 'x'.repeat(REVIEW_LIMITS.fileBytes + 1), after: '', complete: true }),
    ).toBe(true);
  });

  it('more than 300 files keep the tree folded', () => {
    expect(autoExpandTree(300)).toBe(true);
    expect(autoExpandTree(301)).toBe(false);
  });
});
