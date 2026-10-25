// What the person picked out of the agent's answers to answer to: a quote, or a quote with their remark. It sits as a
// chip in the field until the message goes and then leads its text as a Markdown quote. Kept for the page only.

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { ConvId } from '@/next/protocol/v2';

export interface Quote {
  id: string;
  text: string;
  note: string | null;
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
