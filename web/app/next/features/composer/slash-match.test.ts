import { describe, expect, it } from 'vitest';
import { bestMatch, fuzzyMatch, highlighted } from '@/next/features/composer/slash-match';

describe('fuzzyMatch', () => {
  it('matches everything for an empty query and marks nothing', () => {
    expect(fuzzyMatch('', 'compact')).toEqual({ score: 0, ranges: [] });
  });

  it('marks a whole run of the query', () => {
    expect(fuzzyMatch('pac', 'compact')?.ranges).toEqual([[3, 6]]);
  });

  it('takes the letters in order, apart, and marks each run', () => {
    expect(fuzzyMatch('cmp', 'compact')?.ranges).toEqual([
      [0, 1],
      [2, 4],
    ]);
  });

  it('does not match letters out of order or missing ones', () => {
    expect(fuzzyMatch('tcp', 'compact')).toBeNull();
    expect(fuzzyMatch('z', 'compact')).toBeNull();
  });

  it('ranks a prefix over a start of a word over the middle of a word', () => {
    const prefix = fuzzyMatch('com', 'commit')?.score ?? 0;
    const word = fuzzyMatch('com', 'git:commit')?.score ?? 0;
    const inside = fuzzyMatch('com', 'recommend')?.score ?? 0;
    expect(prefix).toBeGreaterThan(word);
    expect(word).toBeGreaterThan(inside);
  });

  it('ignores case', () => {
    expect(fuzzyMatch('MOD', 'model')?.ranges).toEqual([[0, 3]]);
  });

  it('prefers the start of a word to an earlier letter inside one', () => {
    expect(fuzzyMatch('cc', 'clear-context')?.ranges).toEqual([
      [0, 1],
      [6, 7],
    ]);
  });
});

describe('bestMatch', () => {
  it('names the spelling that matched best', () => {
    expect(bestMatch('res', ['clear', 'reset'])?.name).toBe('reset');
    expect(bestMatch('qq', ['clear', 'reset'])).toBeNull();
  });
});

describe('highlighted', () => {
  it('cuts the text at the ranges', () => {
    expect(
      highlighted('compact', [
        [0, 1],
        [2, 4],
      ]),
    ).toEqual([
      { text: 'c', hit: true },
      { text: 'o', hit: false },
      { text: 'mp', hit: true },
      { text: 'act', hit: false },
    ]);
  });

  it('is the whole text when nothing matched', () => {
    expect(highlighted('compact', [])).toEqual([{ text: 'compact', hit: false }]);
  });
});
