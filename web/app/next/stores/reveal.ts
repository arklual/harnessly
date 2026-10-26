// The message a search result points at: the chat opens scrolled to it, and the row shows it for a moment. Kept for the
// page only; the feed of that chat takes it when it is on screen and gives it back when it has shown it (or found it
// is not in the chat any more).
import { createStore, useStore } from '@/next/core/store';
import type { ConvId, EntryId, TurnId } from '@/next/protocol/v2';

export interface Reveal {
  conv: ConvId;
  entryId: EntryId;
  /** The turn the entry is in, null for one outside any. */
  turnId: TurnId | null;
  /** Tells one ask from the next for the same message. */
  nonce: number;
}

const revealStore = createStore<Reveal | null>(null);

let asked = 0;

export function revealMessage(target: Omit<Reveal, 'nonce'>): void {
  asked += 1;
  revealStore.set({ ...target, nonce: asked });
}

/** The ask has been served (or cannot be): it is no more. */
export function doneReveal(nonce: number): void {
  revealStore.update((now) => (now?.nonce === nonce ? null : now));
}

/** The ask for a message of this chat, if there is one. */
export function useReveal(conv: ConvId): Reveal | null {
  const now = useStore(revealStore);
  return now !== null && now.conv === conv ? now : null;
}
