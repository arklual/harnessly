import { describe, expect, it } from 'vitest';
import { diffLines, diffStat, unifiedDiff } from '@/lib/line-diff';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';

describe('diffLines', () => {
  it('marks only the lines that changed', () => {
    const lines = diffLines('a\nb\nc\n', 'a\nB\nc\n');
    expect(lines).toEqual([
      { kind: ' ', text: 'a' },
      { kind: '-', text: 'b' },
      { kind: '+', text: 'B' },
      { kind: ' ', text: 'c' },
    ]);
    expect(diffStat(lines)).toEqual({ added: 1, removed: 1 });
  });

  it('keeps a moved-around middle aligned by common lines', () => {
    const lines = diffLines('1\n2\n3\n4\n', '1\n3\n4\n5\n');
    expect(diffStat(lines)).toEqual({ added: 1, removed: 1 });
  });

  it('treats a write into nothing as all added', () => {
    expect(diffStat(diffLines('', 'x\ny'))).toEqual({ added: 2, removed: 0 });
  });

  it('falls back to a whole replacement on huge inputs', () => {
    const a = Array.from({ length: 3000 }, (_, i) => `a${i}`).join('\n');
    const b = Array.from({ length: 3000 }, (_, i) => `b${i}`).join('\n');
    expect(diffStat(diffLines(a, b))).toEqual({ added: 3000, removed: 3000 });
  });
});

describe('unifiedDiff', () => {
  it('is empty when nothing differs', () => {
    expect(unifiedDiff('x.ts', 'a\n', 'a\n')).toBe('');
  });

  it('writes one hunk with context that the diff parser reads', () => {
    const old = Array.from({ length: 10 }, (_, i) => `l${i}`).join('\n');
    const next = old.replace('l5', 'L5');
    const diff = unifiedDiff('src/x.ts', old, next);
    expect(diff).toContain('@@ -3,7 +3,7 @@');
    const [file] = parseMultiFileDiff(diff);
    expect(file).toMatchObject({ newPath: 'src/x.ts', additions: 1, deletions: 1, isNew: false });
  });

  it('splits distant changes into separate hunks', () => {
    const old = Array.from({ length: 30 }, (_, i) => `l${i}`).join('\n');
    const next = old.replace('l2', 'X').replace('l25', 'Y');
    expect(unifiedDiff('f', old, next).match(/^@@/gm)).toHaveLength(2);
  });

  it('marks a created file as new', () => {
    const [file] = parseMultiFileDiff(unifiedDiff('new.md', '', 'one\ntwo\n'));
    expect(file).toMatchObject({ isNew: true, additions: 2, deletions: 0 });
  });
});
