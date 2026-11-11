// What the person picked out of the agent's answers to answer to: a quote, or a quote with their remark. It sits as a
// chip in the field until the message goes and then leads its text as a Markdown quote. Kept for the page only. A quote
// picked in the chat's feed keeps where its words are (an annotation): the feed marks them with its number while the
// quote is in the field.

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { IFeedAnchor } from '@/next/features/chat/feed-anchor';
import type { ConvId } from '@/next/protocol/v2';

export interface Quote {
  id: string;
  text: string;
  note: string | null;
  /** Where the words stand in the feed: an annotation of the feed, numbered there and in the field. */
  anchor?: IFeedAnchor;
}

const NONE: readonly Quote[] = [];

export const quotesStore = createKeyedStore<ConvId, readonly Quote[]>();

export const useQuotes = (conv: ConvId | null): readonly Quote[] => useKey(quotesStore, conv) ?? NONE;

let seq = 0;

export function addQuote(conv: ConvId, text: string, note: string | null): void {
  seq += 1;
  quotesStore.patch(conv, (have) => [...(have ?? NONE), { id: `q${seq}`, text, note }]);
  flush();
}

/** A quote under a name its giver keeps (a review's comment put into the field), so the same name takes it out again. */
export function putQuote(conv: ConvId, quote: Pick<Quote, 'id' | 'text' | 'note'>): void {
  quotesStore.patch(conv, (have) => [...(have ?? NONE).filter((q) => q.id !== quote.id), quote]);
  flush();
}

/** A name for a feed annotation, given before it is in the field: its mark keeps it from the first press. */
export const nextFeedNoteId = (): string => {
  seq += 1;
  return `q${seq}`;
};

/** An annotation of the feed: the words, where they stand, the remark; it goes after the quotes the field has. */
export function addFeedNote(
  conv: ConvId,
  note: Required<Pick<Quote, 'id' | 'text' | 'note' | 'anchor'>>,
): void {
  quotesStore.patch(conv, (have) => [...(have ?? NONE), note]);
  flush();
}

/** The remark of a quote, changed (an empty one leaves the words alone). */
export function editQuote(conv: ConvId, id: string, note: string): void {
  quotesStore.patch(conv, (have) =>
    (have ?? NONE).map((q) => (q.id === id ? { ...q, note: note === '' ? null : note } : q)),
  );
}

/** The feed's annotations, numbered 1, 2, 3… in the order they were put. */
export const feedNotesOf = (quotes: readonly Quote[]): Quote[] =>
  quotes.filter((q) => q.anchor !== undefined);

/** «Очистить»: the feed's annotations go, the other quotes stay. */
export function clearFeedNotes(conv: ConvId): void {
  quotesStore.patch(conv, (have) => {
    const left = (have ?? NONE).filter((q) => q.anchor === undefined);
    return left.length > 0 ? left : undefined;
  });
}

export function removeQuote(conv: ConvId, id: string): void {
  quotesStore.patch(conv, (have) => {
    const left = (have ?? NONE).filter((q) => q.id !== id);
    return left.length > 0 ? left : undefined;
  });
}

export const clearQuotes = (conv: ConvId): void => quotesStore.delete(conv);

const shorten = (text: string, n: number): string =>
  text.length > n ? `${text.slice(0, n - 1).trim()}…` : text;

/** The chip's words: the quote in guillemets; with a remark, the remark leads and the quote follows after a dot. */
export const quoteLabel = (q: Quote): string => {
  if (!q.note) return `«${shorten(q.text, 60)}»`;
  return `${shorten(q.note, 40)} · «${shorten(q.text, 24)}»`;
};

/** The chip's hint: the whole quote, and the remark under it. */
export const quoteTitle = (q: Quote): string => (q.note ? `«${q.text}»\n\n${q.note}` : `«${q.text}»`);

/** The message as it goes: every quote as a Markdown quote with its remark under it, then the person's text. */
export const withQuotes = (quotes: readonly Quote[], text: string): string => {
  const parts = quotes.map((q) => {
    const quoted = q.text
      .split('\n')
      .map((line) => `> ${line}`)
      .join('\n');
    return q.note ? `${quoted}\n\n${q.note}` : quoted;
  });
  if (text) parts.push(text);
  return parts.join('\n\n');
};
