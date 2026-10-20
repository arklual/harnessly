import { describe, expect, it } from 'vitest';
import { optionNumber, parseChoiceOptions, stripNumberPrefix } from '@/lib/permission-prompt';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';
import { formatModelDisplayName } from '@/lib/claude-tokens';

describe('permission prompt', () => {
  it('reads the last numbered menu and its highlighted option', () => {
    const screen = [
      'Do you want to proceed?',
      '❯ 1. Yes',
      "  2. Yes, and don't ask again",
      '  3. No, and tell Claude',
      '',
    ].join('\n');
    expect(parseChoiceOptions(screen)).toEqual({
      options: ['1. Yes', "2. Yes, and don't ask again", '3. No, and tell Claude'],
      focusedIndex: 0,
    });
    expect(optionNumber('❯ 2. No')).toBe('2');
    expect(stripNumberPrefix('  3. Later')).toBe('Later');
  });

  it('finds nothing in plain text', () => {
    expect(parseChoiceOptions('just output\n1. alone').options).toEqual([]);
  });
});

describe('parseMultiFileDiff', () => {
  it('splits a diff per file and counts lines', () => {
    const diff = [
      'diff --git a/a.ts b/a.ts',
      '--- a/a.ts',
      '+++ b/a.ts',
      '@@ -1,2 +1,2 @@',
      '-old',
      '+new',
      '+more',
      'diff --git a/b.md b/b.md',
      'new file mode 100644',
      '--- /dev/null',
      '+++ b/b.md',
      '@@ -0,0 +1 @@',
      '+hi',
    ].join('\n');
    const files = parseMultiFileDiff(diff);
    expect(files.map((f) => [f.newPath, f.additions, f.deletions, f.isNew])).toEqual([
      ['a.ts', 2, 1, false],
      ['b.md', 1, 0, true],
    ]);
  });
});

describe('formatModelDisplayName', () => {
  it('names Claude models the way people read them', () => {
    expect(formatModelDisplayName('claude-opus-4-5-20251101')).toBe('Opus 4.5');
    expect(formatModelDisplayName('claude-sonnet-4-20250514')).toBe('Sonnet 4');
    expect(formatModelDisplayName('gpt-5')).toBe('gpt-5');
  });
});
