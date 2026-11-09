import { describe, expect, it } from 'vitest';
import type { ConvId } from '@/next/protocol/v2';
import {
  addFeedNote,
  addQuote,
  clearFeedNotes,
  editQuote,
  feedNotesOf,
  nextFeedNoteId,
  quoteLabel,
  quotesStore,
  quoteTitle,
  withQuotes,
} from '@/next/stores/quotes';

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

  it('keep the feed’s annotations numbered apart, with their remarks, and clear only them', () => {
    const conv = 'c-feed' as ConvId;
    addQuote(conv, 'from a file', null);
    const anchor = { key: 't1', start: 0, raw: 'words' };
    const id = nextFeedNoteId();
    addFeedNote(conv, { id, text: 'words', note: null, anchor });
    editQuote(conv, id, 'why so?');
    const have = quotesStore.get(conv) ?? [];
    expect(feedNotesOf(have)).toEqual([{ id, text: 'words', note: 'why so?', anchor }]);
    editQuote(conv, id, '');
    expect(feedNotesOf(quotesStore.get(conv) ?? [])[0]?.note).toBeNull();
    clearFeedNotes(conv);
    expect((quotesStore.get(conv) ?? []).map((q) => q.text)).toEqual(['from a file']);
  });
});
