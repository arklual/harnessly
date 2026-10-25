import { describe, expect, it } from 'vitest';
import { quoteLabel, quoteTitle, withQuotes } from '@/next/stores/quotes';

describe('quotes', () => {
  it('lead the message as Markdown quotes, a remark under its quote', () => {
    const text = withQuotes(
      [
        { id: 'q1', text: 'first line\nsecond line', note: null },
        { id: 'q2', text: 'this part', note: 'is wrong' },
      ],
      'fix both',
    );
    expect(text).toBe('> first line\n> second line\n\n> this part\n\nis wrong\n\nfix both');
  });

  it('send a bare quote without an empty tail', () => {
    expect(withQuotes([{ id: 'q', text: 'only', note: null }], '')).toBe('> only');
    expect(withQuotes([], 'plain')).toBe('plain');
  });

  it('name a chip by the quote, shortened; with a remark the remark leads and the quote follows', () => {
    expect(quoteLabel({ id: 'q', text: 'short', note: null })).toBe('«short»');
    expect(quoteLabel({ id: 'q', text: 'short', note: 'wrong' })).toBe('wrong · «short»');
    const long = quoteLabel({ id: 'q', text: 'x'.repeat(80), note: 'y'.repeat(60) });
    expect(long.startsWith(`${'y'.repeat(39)}… · «${'x'.repeat(23)}…»`)).toBe(true);
  });

  it('hint a chip with the whole quote and the remark under it', () => {
    expect(quoteTitle({ id: 'q', text: 'the part', note: null })).toBe('«the part»');
    expect(quoteTitle({ id: 'q', text: 'the part', note: 'is wrong' })).toBe('«the part»\n\nis wrong');
  });
});
