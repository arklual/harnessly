// The block being noted is the page's: each page of a design keeps its own picked block and card, so a
// switch to another page hides them (with their exit) and a return brings them back where they were, the words typed
// in the card included — never the frame of one page over another page's block.
import { useState } from 'react';
import type { INoteDraft } from '@/next/features/designly/site-notes';

export type TPageDrafts = Readonly<Record<string, INoteDraft>>;

export const withPageDraft = (have: TPageDrafts, page: string, next: INoteDraft | null): TPageDrafts => {
  const rest = Object.fromEntries(Object.entries(have).filter(([key]) => key !== page));
  return next ? { ...rest, [page]: next } : rest;
};

/** The card of the page on screen and its setter; the other pages' cards wait for their pages. */
export const usePageDraft = (page: string): [INoteDraft | null, (next: INoteDraft | null) => void] => {
  const [drafts, setDrafts] = useState<TPageDrafts>({});
  return [drafts[page] ?? null, (next) => setDrafts((have) => withPageDraft(have, page, next))];
};
