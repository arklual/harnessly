// The decks the page has open, keyed by the project's id: the runtime's state (`GET /design/deck`) as last read, and
// the changes the page made since (a saved note, an added or resolved comment) laid over it until the next read. A
// deck not read yet has no entry: its screens show their loading state, not an empty deck. Kept for the page only.
import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type {
  IDeckComment,
  IDeckSettings,
  IDeckSlide,
  IDeckState,
} from '@/next/features/designly/deck/types';

export const deckStore = createKeyedStore<string, IDeckState>();

/** The deck as last read; undefined until the first read. */
export const useDeck = (artifact: string | null): IDeckState | undefined => useKey(deckStore, artifact);

/** A fresh read: an older `rev` than the one held changes nothing (a poll that crossed a write). */
export function setDeck(artifact: string, state: IDeckState): void {
  deckStore.patch(artifact, (have) => (have && have.rev > state.rev ? have : state));
  flush();
}

/** Changes a deck the page holds; nothing for one not read yet. */
export function changeDeck(artifact: string, change: (have: IDeckState) => IDeckState): void {
  deckStore.patch(artifact, (have) => (have ? change(have) : undefined));
  flush();
}

export const putDeckSlide = (artifact: string, slide: IDeckSlide): void =>
  changeDeck(artifact, (d) => ({ ...d, slides: d.slides.map((s) => (s.id === slide.id ? slide : s)) }));

export const putDeckSettingsLocal = (artifact: string, settings: IDeckSettings): void =>
  changeDeck(artifact, (d) => ({ ...d, settings }));

/** Adds a comment or replaces the one with its id. */
export const putDeckComment = (artifact: string, comment: IDeckComment): void =>
  changeDeck(artifact, (d) => {
    const at = d.comments.findIndex((c) => c.id === comment.id);
    const comments = at < 0 ? [...d.comments, comment] : d.comments.map((c, i) => (i === at ? comment : c));
    return { ...d, comments };
  });

/** Marks a guest comment (`g-<id>`) resolved or not. */
export const putGuestResolved = (artifact: string, id: string, resolved: boolean): void =>
  changeDeck(artifact, (d) => {
    const rest = d.guestResolved.filter((x) => x !== id);
    return { ...d, guestResolved: resolved ? [...rest, id] : rest };
  });

export const dropDeck = (artifact: string): void => {
  deckStore.delete(artifact);
  flush();
};
