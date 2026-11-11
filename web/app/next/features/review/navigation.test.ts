import { describe, expect, it } from 'vitest';
import { FILE_STATUS, type IReviewFile } from '@/next/features/review/model';
import { navigate, NAV_DIR, neighbourFile, nextChangeLine } from '@/next/features/review/navigation';

const file = (path: string): IReviewFile => ({
  path,
  oldPath: path,
  status: FILE_STATUS.modified,
  additions: 1,
  deletions: 0,
  binary: false,
  patch: '',
  version: 'v',
});
const order = [file('a.ts'), file('b.ts'), file('c.ts')];

describe('F7 and Shift+F7', () => {
  it('finds the next and the previous change from the cursor', () => {
    expect(nextChangeLine([40, 10, 25], 0, NAV_DIR.next)).toBe(10);
    expect(nextChangeLine([10, 25, 40], 10, NAV_DIR.next)).toBe(25);
    expect(nextChangeLine([10, 25, 40], 40, NAV_DIR.next)).toBeNull();
    expect(nextChangeLine([10, 25, 40], 30, NAV_DIR.prev)).toBe(25);
    expect(nextChangeLine([10, 25, 40], 10, NAV_DIR.prev)).toBeNull();
  });

  it('stays in the file while it has changes ahead', () => {
    expect(navigate({ changeLines: [5, 9], cursor: 5, order, current: 'b.ts', dir: NAV_DIR.next })).toEqual({
      kind: 'line',
      line: 9,
    });
  });

  it('goes to the first change of the next file past the last one', () => {
    expect(navigate({ changeLines: [5, 9], cursor: 9, order, current: 'b.ts', dir: NAV_DIR.next })).toEqual({
      kind: 'file',
      path: 'c.ts',
      edge: 'first',
    });
  });

  it('goes to the last change of the previous file before the first one', () => {
    expect(navigate({ changeLines: [5, 9], cursor: 5, order, current: 'b.ts', dir: NAV_DIR.prev })).toEqual({
      kind: 'file',
      path: 'a.ts',
      edge: 'last',
    });
  });

  it('a file just opened starts before its first change', () => {
    expect(navigate({ changeLines: [1], cursor: 0, order, current: 'a.ts', dir: NAV_DIR.next })).toEqual({
      kind: 'line',
      line: 1,
    });
  });

  it('stops at the ends of the list', () => {
    expect(navigate({ changeLines: [3], cursor: 3, order, current: 'c.ts', dir: NAV_DIR.next })).toBeNull();
    expect(navigate({ changeLines: [3], cursor: 3, order, current: 'a.ts', dir: NAV_DIR.prev })).toBeNull();
    expect(
      navigate({ changeLines: [], cursor: 0, order: [file('a.ts')], current: 'a.ts', dir: NAV_DIR.next }),
    ).toBeNull();
  });

  it('starts from an end when no file is open', () => {
    expect(neighbourFile(order, null, NAV_DIR.next)?.path).toBe('a.ts');
    expect(neighbourFile(order, null, NAV_DIR.prev)?.path).toBe('c.ts');
  });
});
