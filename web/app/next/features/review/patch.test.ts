import { describe, expect, it } from 'vitest';
import { filesFromPatch, hashText, sidesFromPatch } from '@/next/features/review/patch';

const DIFF = [
  'diff --git a/src/a.ts b/src/a.ts',
  'index 111..222 100644',
  '--- a/src/a.ts',
  '+++ b/src/a.ts',
  '@@ -2,3 +2,3 @@',
  ' two',
  '-three',
  '+THREE',
  ' four',
  '@@ -9,2 +9,3 @@',
  ' nine',
  '+nine and a half',
  ' ten',
  'diff --git a/gone.ts b/gone.ts',
  'deleted file mode 100644',
  '--- a/gone.ts',
  '+++ /dev/null',
  '@@ -1 +0,0 @@',
  '-bye',
  'diff --git a/old.ts b/new.ts',
  'similarity index 100%',
  'rename from old.ts',
  'rename to new.ts',
  '',
].join('\n');

describe('files from a patch', () => {
  it('reads paths, status and counts', () => {
    const files = filesFromPatch(DIFF);
    expect(files.map((f) => [f.path, f.status, f.additions, f.deletions])).toEqual([
      ['src/a.ts', 'modified', 2, 1],
      ['gone.ts', 'deleted', 0, 1],
      ['new.ts', 'renamed', 0, 0],
    ]);
  });

  it('versions a file by its hunks, not by its index line', () => {
    const [a] = filesFromPatch(DIFF);
    const [same] = filesFromPatch(DIFF.replace('index 111..222', 'index 333..444'));
    const [changed] = filesFromPatch(DIFF.replace('+THREE', '+3'));
    expect(same!.version).toBe(a!.version);
    expect(changed!.version).not.toBe(a!.version);
    expect(hashText('abc')).toMatch(/^[0-9a-f]{8}$/);
  });
});

describe('sides from a patch', () => {
  it('keeps line numbers, leaving unknown lines blank', () => {
    const [a] = filesFromPatch(DIFF);
    const sides = sidesFromPatch(a!.patch);
    const before = sides.before.split('\n');
    const after = sides.after.split('\n');
    expect(sides.complete).toBe(false);
    expect(before[0]).toBe('');
    expect(before.slice(1, 4)).toEqual(['two', 'three', 'four']);
    expect(after.slice(1, 4)).toEqual(['two', 'THREE', 'four']);
    expect(before[8]).toBe('nine');
    expect(after.slice(8, 11)).toEqual(['nine', 'nine and a half', 'ten']);
  });

  it('ignores the no-newline marker', () => {
    const sides = sidesFromPatch('@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n');
    expect(sides).toEqual({ before: 'a', after: 'b', complete: false });
  });
});
