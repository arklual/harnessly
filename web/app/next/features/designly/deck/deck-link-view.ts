// What a deck's guest link carries in its `view` (the gateway keeps it as is and hands it back in the guest config):
// how the link opens, and the flags the gateway gates its files by (`video`, `pdf`) and its counting by (`stats`).
// Pure: the share popover writes it, the guest app reads it.
import type { IGuestDeckView } from '@/next/features/designly/access/api';
import { DECK_OPENS, type TDeckOpens } from '@/next/features/designly/deck/types';

export interface IDeckLinkFlags {
  opens: TDeckOpens;
  /** Guests may download the published PDF. */
  pdf: boolean;
  /** Who looked and for how long is counted. */
  stats: boolean;
}

/** A link made without a say: the board's toggles start on. */
export const DECK_LINK_DEFAULT: IDeckLinkFlags = { opens: DECK_OPENS.slides, pdf: true, stats: true };

const OPENS: readonly string[] = Object.values(DECK_OPENS);

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/** The `view` of a link for these flags. `video` is always on: the gateway serves the recording only once the owner has published one. */
export const deckViewOf = (flags: IDeckLinkFlags): IGuestDeckView => ({
  v: 1,
  deck: flags.opens,
  pdf: flags.pdf,
  video: true,
  stats: flags.stats,
});

/** The flags a link's `view` holds; null where the link is not a deck's (no `deck` mode in it). */
export const readDeckLinkView = (view: unknown): IDeckLinkFlags | null => {
  if (!isRecord(view) || typeof view.deck !== 'string' || !OPENS.includes(view.deck)) return null;
  return {
    opens: view.deck as TDeckOpens,
    pdf: view.pdf === true,
    stats: view.stats === true,
  };
};

/** The mode the guest page starts in: «Как откроется», or the slides where the recording is not there to open. */
export const startOpens = (view: unknown, hasVideo: boolean): TDeckOpens => {
  const flags = readDeckLinkView(view);
  if (!flags) return DECK_OPENS.slides;
  return flags.opens === DECK_OPENS.video && !hasVideo ? DECK_OPENS.slides : flags.opens;
};

/** Whether the guest's «Ещё» offers the PDF: the owner allowed it and the deck has one published. */
export const pdfAllowed = (view: unknown, hasPdf: boolean): boolean =>
  hasPdf && isRecord(view) && view.pdf === true;

/** Whether the guest page reports what a guest looked at. */
export const statsOn = (view: unknown): boolean => isRecord(view) && view.stats === true;
