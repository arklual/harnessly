// A queued message taken back into the field («Изменить») keeps the design's notes it had: its words go into the text,
// its `<annotation>` blocks wait here as chips and go with the next message as they were, renumbered with the notes made
// since. The person never edits the markup by hand. Kept for the page only.

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { ISentNote } from '@/next/features/designly/annotation';
import type { ConvId } from '@/next/protocol/v2';

const NONE: readonly ISentNote[] = [];

export const carriedNotesStore = createKeyedStore<ConvId, readonly ISentNote[]>();

export const useCarriedNotes = (conv: ConvId | null): readonly ISentNote[] =>
  useKey(carriedNotesStore, conv) ?? NONE;

export function carryNotes(conv: ConvId, notes: readonly ISentNote[]): void {
  if (notes.length === 0) return;
  carriedNotesStore.patch(conv, (have) => [...(have ?? NONE), ...notes]);
  flush();
}

/** Takes back the note at `index` of the chat's list. */
export function dropCarriedNote(conv: ConvId, index: number): void {
  carriedNotesStore.patch(conv, (have) => {
    const left = (have ?? NONE).filter((_, i) => i !== index);
    return left.length > 0 ? left : undefined;
  });
  flush();
}

export const clearCarriedNotes = (conv: ConvId): void => carriedNotesStore.delete(conv);

/** The chip id of a carried note; the index is its place in the list. */
export const CARRIED_PREFIX = 'carried:';
