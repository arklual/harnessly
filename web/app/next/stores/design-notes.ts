// The notes the person made on a design's blocks in the right pane, kept for the chat the pane is beside: the pane draws
// their pins and writes them, the chat's own field shows them as chips and sends them with its message (as the
// `<annotation>` blocks the agent reads). The chat on the left is where the edits are asked for, so the notes live
// under its id, not the design's. Kept for the page only.

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { INote } from '@/next/features/designly/site-notes';
import type { ConvId } from '@/next/protocol/v2';

export interface IDesignNote extends INote {
  /** The library server's port on the machine, where the page the note is on is served. */
  port: number;
}

const NONE: readonly IDesignNote[] = [];

export const designNotesStore = createKeyedStore<ConvId, readonly IDesignNote[]>();

export const useDesignNotes = (conv: ConvId | null): readonly IDesignNote[] =>
  useKey(designNotesStore, conv) ?? NONE;

/** Changes the chat's list; an empty one is dropped. */
export function patchDesignNotes(
  conv: ConvId,
  change: (have: readonly IDesignNote[]) => readonly IDesignNote[],
): void {
  designNotesStore.patch(conv, (have) => {
    const next = change(have ?? NONE);
    return next.length > 0 ? next : undefined;
  });
  flush();
}

export const clearDesignNotes = (conv: ConvId): void => {
  designNotesStore.delete(conv);
  flush();
};
